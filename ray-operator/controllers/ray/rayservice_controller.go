package ray

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/json"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

	cmap "github.com/orcaman/concurrent-map"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

// This variable is mutable for unit testing purpose.
var (
	ServiceUnhealthySecondThreshold                                 = 60.0 // Serve deployment related health check.
	serveConfigTypeForTesting       *rayv1alpha1.RayServeConfigType = nil
)

const (
	ServiceDefaultRequeueDuration      = 2 * time.Second
	ServiceRestartRequeueDuration      = 10 * time.Second
	RayClusterDeletionDelayDuration    = 60 * time.Second
	DeploymentUnhealthySecondThreshold = 60.0 // Dashboard agent related health check.
)

// RayServiceReconciler reconciles a RayService object
type RayServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
	// Currently, the Ray dashboard doesn't cache the Serve deployment config.
	// To avoid reapplying the same config repeatedly, cache the config in this map.
	ServeConfigs                 cmap.ConcurrentMap
	RayClusterDeletionTimestamps cmap.ConcurrentMap
}

// NewRayServiceReconciler returns a new reconcile.Reconciler
func NewRayServiceReconciler(mgr manager.Manager) *RayServiceReconciler {
	return &RayServiceReconciler{
		Client:                       mgr.GetClient(),
		Scheme:                       mgr.GetScheme(),
		Log:                          ctrl.Log.WithName("controllers").WithName("RayService"),
		Recorder:                     mgr.GetEventRecorderFor("rayservice-controller"),
		ServeConfigs:                 cmap.New(),
		RayClusterDeletionTimestamps: cmap.New(),
	}
}

// +kubebuilder:rbac:groups=ray.io,resources=rayservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups=extensions,resources=ingresses,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;delete;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;delete

// [WARNING]: There MUST be a newline after kubebuilder markers.
// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// This the top level reconciliation flow for RayService.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *RayServiceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("ServiceName", request.NamespacedName)
	var isHealthy, isReady bool = false, false

	var rayServiceInstance *rayv1alpha1.RayService
	var err error
	var ctrlResult ctrl.Result

	// Resolve the CR from request.
	if rayServiceInstance, err = r.getRayServiceInstance(ctx, request); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	originalRayServiceInstance := rayServiceInstance.DeepCopy()
	r.cleanUpServeConfigCache(rayServiceInstance)

	// TODO (kevin85421): ObservedGeneration should be used to determine whether to update this CR or not.
	rayServiceInstance.Status.ObservedGeneration = rayServiceInstance.ObjectMeta.Generation

	logger.Info("Reconciling the cluster component.")
	// Find active and pending ray cluster objects given current service name.
	var activeRayClusterInstance *rayv1alpha1.RayCluster
	var pendingRayClusterInstance *rayv1alpha1.RayCluster
	if activeRayClusterInstance, pendingRayClusterInstance, err = r.reconcileRayCluster(ctx, rayServiceInstance); err != nil {
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailedToGetOrCreateRayCluster, err)
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, client.IgnoreNotFound(err)
	}

	// Check if we need to create pending RayCluster.
	if rayServiceInstance.Status.PendingServiceStatus.RayClusterName != "" && pendingRayClusterInstance == nil {
		// Update RayService Status since reconcileRayCluster may mark RayCluster restart.
		if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
			logger.Error(errStatus, "Fail to update status of RayService after RayCluster changes", "rayServiceInstance", rayServiceInstance)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, nil
		}
		logger.Info("Done reconcileRayCluster update status, enter next loop to create new ray cluster.")
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, nil
	}

	/*
		Update ray cluster for 4 possible situations.
		If a ray cluster does not exist, clear its status.
		If only one ray cluster exists, do serve deployment if needed and check dashboard, serve deployment health.
		If both ray clusters exist, update active cluster status and do the pending cluster deployment and health check.
	*/
	if activeRayClusterInstance != nil && pendingRayClusterInstance == nil {
		logger.Info("Reconciling the Serve component. Only the active Ray cluster exists.")
		rayServiceInstance.Status.PendingServiceStatus = rayv1alpha1.RayServiceStatus{}
		if ctrlResult, isHealthy, isReady, err = r.reconcileServe(ctx, rayServiceInstance, activeRayClusterInstance, true, logger); err != nil {
			logger.Error(err, "Fail to reconcileServe.")
			return ctrlResult, nil
		}
	} else if activeRayClusterInstance != nil && pendingRayClusterInstance != nil {
		logger.Info("Reconciling the Serve component. Active and pending Ray clusters exist.")
		if err = r.updateStatusForActiveCluster(ctx, rayServiceInstance, activeRayClusterInstance, logger); err != nil {
			logger.Error(err, "Failed to update active Ray cluster's status.")
		}

		if ctrlResult, isHealthy, isReady, err = r.reconcileServe(ctx, rayServiceInstance, pendingRayClusterInstance, false, logger); err != nil {
			logger.Error(err, "Fail to reconcileServe.")
			return ctrlResult, nil
		}
	} else if activeRayClusterInstance == nil && pendingRayClusterInstance != nil {
		rayServiceInstance.Status.ActiveServiceStatus = rayv1alpha1.RayServiceStatus{}
		if ctrlResult, isHealthy, isReady, err = r.reconcileServe(ctx, rayServiceInstance, pendingRayClusterInstance, false, logger); err != nil {
			logger.Error(err, "Fail to reconcileServe.")
			return ctrlResult, nil
		}
	} else {
		logger.Info("Reconciling the Serve component. No Ray cluster exists.")
		rayServiceInstance.Status.ActiveServiceStatus = rayv1alpha1.RayServiceStatus{}
		rayServiceInstance.Status.PendingServiceStatus = rayv1alpha1.RayServiceStatus{}
	}

	if !isHealthy {
		logger.Info(fmt.Sprintf("Cluster is not healthy: checking again in %s", ServiceRestartRequeueDuration))
		r.Recorder.Eventf(rayServiceInstance, "Normal", "ServiceUnhealthy", "The service is in an unhealthy state. Controller will perform a round of actions in %s.", ServiceRestartRequeueDuration)
		return ctrl.Result{RequeueAfter: ServiceRestartRequeueDuration}, nil
	} else if !isReady {
		logger.Info(fmt.Sprintf("Cluster is healthy but not ready: checking again in %s", ServiceDefaultRequeueDuration))
		r.Recorder.Eventf(rayServiceInstance, "Normal", "ServiceNotReady", "The service is not ready yet. Controller will perform a round of actions in %s.", ServiceDefaultRequeueDuration)
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, nil
	}

	// Get the ready Ray cluster instance for service and ingress update.
	var rayClusterInstance *rayv1alpha1.RayCluster
	if pendingRayClusterInstance != nil {
		rayClusterInstance = pendingRayClusterInstance
		logger.Info("Reconciling the ingress and service resources " +
			"on the pending Ray cluster.")
	} else if activeRayClusterInstance != nil {
		rayClusterInstance = activeRayClusterInstance
		logger.Info("Reconciling the ingress and service resources " +
			"on the active Ray cluster. No pending Ray cluster found.")
	} else {
		rayClusterInstance = nil
		logger.Info("No Ray cluster found. Skipping ingress and service reconciliation.")
	}

	if rayClusterInstance != nil {
		if err := r.reconcileIngress(ctx, rayServiceInstance, rayClusterInstance); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailedToUpdateIngress, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
		if err := r.reconcileServices(ctx, rayServiceInstance, rayClusterInstance, common.HeadService); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailedToUpdateService, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
		if err := r.labelHealthyServePods(ctx, rayClusterInstance); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailedToUpdateServingPodLabel, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
		if err := r.reconcileServices(ctx, rayServiceInstance, rayClusterInstance, common.ServingService); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailedToUpdateService, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
	}

	// Final status update for any CR modification.
	if r.inconsistentRayServiceStatuses(originalRayServiceInstance.Status, rayServiceInstance.Status) {
		if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
			logger.Error(errStatus, "Failed to update RayService status", "rayServiceInstance", rayServiceInstance)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, errStatus
		}
	}

	return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, nil
}

// Checks whether the old and new RayServiceStatus are inconsistent by comparing different fields.
// If the only differences between the old and new status are the LastUpdateTime and HealthLastUpdateTime fields,
// the status update will not be triggered.
// The RayClusterStatus field is only for observability in RayService CR, and changes to it will not trigger the status update.
func (r *RayServiceReconciler) inconsistentRayServiceStatus(oldStatus rayv1alpha1.RayServiceStatus, newStatus rayv1alpha1.RayServiceStatus) bool {
	if oldStatus.RayClusterName != newStatus.RayClusterName {
		r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService RayClusterName changed from %s to %s", oldStatus.RayClusterName, newStatus.RayClusterName))
		return true
	}

	if oldStatus.DashboardStatus.IsHealthy != newStatus.DashboardStatus.IsHealthy {
		r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService DashboardStatus changed from %v to %v", oldStatus.DashboardStatus, newStatus.DashboardStatus))
		return true
	}

	if len(oldStatus.Applications) != len(newStatus.Applications) {
		return true
	}

	var ok bool
	for appName, newAppStatus := range newStatus.Applications {
		var oldAppStatus rayv1alpha1.AppStatus
		if oldAppStatus, ok = oldStatus.Applications[appName]; !ok {
			r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService new application %s found", appName))
			return true
		}

		if oldAppStatus.Status != newAppStatus.Status {
			r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService application %s status changed from %v to %v", appName, oldAppStatus.Status, newAppStatus.Status))
			return true
		} else if oldAppStatus.Message != newAppStatus.Message {
			r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService application %s status message changed from %v to %v", appName, oldAppStatus.Message, newAppStatus.Message))
			return true
		}

		if len(oldAppStatus.Deployments) != len(newAppStatus.Deployments) {
			return true
		}

		for deploymentName, newDeploymentStatus := range newAppStatus.Deployments {
			var oldDeploymentStatus rayv1alpha1.ServeDeploymentStatus
			if oldDeploymentStatus, ok = oldAppStatus.Deployments[deploymentName]; !ok {
				r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService new deployment %s found in application %s", deploymentName, appName))
				return true
			}

			if oldDeploymentStatus.Status != newDeploymentStatus.Status {
				r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService DeploymentStatus changed from %v to %v", oldDeploymentStatus.Status, newDeploymentStatus.Status))
				return true
			} else if oldDeploymentStatus.Message != newDeploymentStatus.Message {
				r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService deployment status message changed from %v to %v", oldDeploymentStatus.Message, newDeploymentStatus.Message))
				return true
			}
		}
	}

	return false
}

// Determine whether to update the status of the RayService instance.
func (r *RayServiceReconciler) inconsistentRayServiceStatuses(oldStatus rayv1alpha1.RayServiceStatuses, newStatus rayv1alpha1.RayServiceStatuses) bool {
	if oldStatus.ServiceStatus != newStatus.ServiceStatus {
		r.Log.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService ServiceStatus changed from %s to %s", oldStatus.ServiceStatus, newStatus.ServiceStatus))
		return true
	}

	if r.inconsistentRayServiceStatus(oldStatus.ActiveServiceStatus, newStatus.ActiveServiceStatus) {
		r.Log.Info("inconsistentRayServiceStatus RayService ActiveServiceStatus changed")
		return true
	}

	if r.inconsistentRayServiceStatus(oldStatus.PendingServiceStatus, newStatus.PendingServiceStatus) {
		r.Log.Info("inconsistentRayServiceStatus RayService PendingServiceStatus changed")
		return true
	}

	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1alpha1.RayService{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		Owns(&rayv1alpha1.RayCluster{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}

func (r *RayServiceReconciler) getRayServiceInstance(ctx context.Context, request ctrl.Request) (*rayv1alpha1.RayService, error) {
	rayServiceInstance := &rayv1alpha1.RayService{}
	if err := r.Get(ctx, request.NamespacedName, rayServiceInstance); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Read request instance not found error!")
		} else {
			r.Log.Error(err, "Read request instance error!")
		}
		return nil, err
	}
	return rayServiceInstance, nil
}

func (r *RayServiceReconciler) updateState(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, status rayv1alpha1.ServiceStatus, err error) error {
	rayServiceInstance.Status.ServiceStatus = status
	if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
		return fmtErrors.Errorf("combined error: %v %v", err, errStatus)
	}
	r.Recorder.Event(rayServiceInstance, "Normal", string(status), err.Error())
	return err
}

// reconcileRayCluster checks the active and pending ray cluster instances. It includes 3 parts.
// 1. It will decide whether to generate a pending cluster name.
// 2. It will delete the old pending ray cluster instance.
// 3. It will create a new pending ray cluster instance.
func (r *RayServiceReconciler) reconcileRayCluster(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService) (*rayv1alpha1.RayCluster, *rayv1alpha1.RayCluster, error) {
	var err error
	if err = r.cleanUpRayClusterInstance(ctx, rayServiceInstance); err != nil {
		return nil, nil, err
	}

	// Get active cluster and pending cluster instances.
	activeRayCluster, err := r.getRayClusterByNamespacedName(ctx, client.ObjectKey{Name: rayServiceInstance.Status.ActiveServiceStatus.RayClusterName, Namespace: rayServiceInstance.Namespace})
	if err != nil {
		return nil, nil, err
	}

	pendingRayCluster, err := r.getRayClusterByNamespacedName(ctx, client.ObjectKey{Name: rayServiceInstance.Status.PendingServiceStatus.RayClusterName, Namespace: rayServiceInstance.Namespace})
	if err != nil {
		return nil, nil, err
	}

	if r.shouldPrepareNewRayCluster(rayServiceInstance, activeRayCluster) {
		r.markRestart(rayServiceInstance)
		return activeRayCluster, nil, nil
	}

	if pendingRayCluster, err = r.createRayClusterInstanceIfNeeded(ctx, rayServiceInstance, pendingRayCluster); err != nil {
		return nil, nil, err
	}

	return activeRayCluster, pendingRayCluster, nil
}

// cleanUpRayClusterInstance cleans up all the dangling RayCluster instances that are owned by the RayService instance.
func (r *RayServiceReconciler) cleanUpRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService) error {
	rayClusterList := rayv1alpha1.RayClusterList{}
	filterLabels := client.MatchingLabels{common.RayServiceLabelKey: rayServiceInstance.Name}
	var err error
	if err = r.List(ctx, &rayClusterList, client.InNamespace(rayServiceInstance.Namespace), filterLabels); err != nil {
		r.Log.Error(err, "Fail to list RayCluster for "+rayServiceInstance.Name)
		return err
	}

	// Clean up RayCluster instances. Each instance is deleted 60 seconds
	// after becoming inactive to give the ingress time to update.
	for _, rayClusterInstance := range rayClusterList.Items {
		if rayClusterInstance.Name != rayServiceInstance.Status.ActiveServiceStatus.RayClusterName && rayClusterInstance.Name != rayServiceInstance.Status.PendingServiceStatus.RayClusterName {
			cachedTimestampObj, exists := r.RayClusterDeletionTimestamps.Get(rayClusterInstance.Name)
			if !exists {
				deletionTimestamp := metav1.Now().Add(RayClusterDeletionDelayDuration)
				r.RayClusterDeletionTimestamps.Set(rayClusterInstance.Name, deletionTimestamp)
				r.Log.V(1).Info(fmt.Sprintf("Scheduled dangling RayCluster "+
					"%s for deletion at %s", rayClusterInstance.Name, deletionTimestamp))
			} else {
				cachedTimestamp, isTimestamp := cachedTimestampObj.(time.Time)
				reasonForDeletion := ""
				if !isTimestamp {
					reasonForDeletion = fmt.Sprintf("Deletion cache contains "+
						"unexpected, non-timestamp object for RayCluster %s. "+
						"Deleting cluster immediately.", rayClusterInstance.Name)
				} else if time.Since(cachedTimestamp) > 0*time.Second {
					reasonForDeletion = fmt.Sprintf("Deletion timestamp %s "+
						"for RayCluster %s has passed. Deleting cluster "+
						"immediately.", cachedTimestamp, rayClusterInstance.Name)
				}

				if reasonForDeletion != "" {
					r.Log.V(1).Info("reconcileRayCluster", "delete Ray cluster", rayClusterInstance.Name, "reason", reasonForDeletion)
					if err := r.Delete(ctx, &rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
						r.Log.Error(err, "Fail to delete RayCluster "+rayClusterInstance.Name)
						return err
					}
				}
			}
		}
	}

	return nil
}

func (r *RayServiceReconciler) getRayClusterByNamespacedName(ctx context.Context, clusterKey client.ObjectKey) (*rayv1alpha1.RayCluster, error) {
	rayCluster := &rayv1alpha1.RayCluster{}
	if clusterKey.Name != "" {
		// Ignore not found since in that case we should return RayCluster as nil.
		if err := r.Get(ctx, clusterKey, rayCluster); client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "Fail to get RayCluster "+clusterKey.String())
			return nil, err
		}
	} else {
		rayCluster = nil
	}

	return rayCluster, nil
}

// cleanUpServeConfigCache cleans up the unused serve deployments config in the cached map.
func (r *RayServiceReconciler) cleanUpServeConfigCache(rayServiceInstance *rayv1alpha1.RayService) {
	activeConfigKey := r.generateConfigKey(rayServiceInstance, rayServiceInstance.Status.ActiveServiceStatus.RayClusterName)
	pendingConfigKey := r.generateConfigKey(rayServiceInstance, rayServiceInstance.Status.PendingServiceStatus.RayClusterName)
	configPrefix := r.generateConfigKeyPrefix(rayServiceInstance)

	// Clean up RayCluster serve deployment configs.
	for key := range r.ServeConfigs.Items() {
		if key == activeConfigKey || key == pendingConfigKey {
			continue
		}
		if !strings.HasPrefix(key, configPrefix) {
			// Skip configs owned by other RayService Instance.
			continue
		}
		r.Log.V(1).Info("cleanUpServeConfigCache", "activeConfigKey", activeConfigKey, "pendingConfigKey", pendingConfigKey, "remove key", key)
		r.ServeConfigs.Remove(key)
	}
}

// shouldPrepareNewRayCluster checks if we need to generate a new pending cluster.
func (r *RayServiceReconciler) shouldPrepareNewRayCluster(rayServiceInstance *rayv1alpha1.RayService, activeRayCluster *rayv1alpha1.RayCluster) bool {
	// Prepare new RayCluster if:
	// 1. No active cluster and no pending cluster
	// 2. No pending cluster, and the active RayCluster has changed.
	if rayServiceInstance.Status.PendingServiceStatus.RayClusterName == "" {
		if activeRayCluster == nil {
			r.Log.Info("No active Ray cluster. RayService operator should prepare a new Ray cluster.")
			return true
		}
		activeClusterHash := activeRayCluster.ObjectMeta.Annotations[common.RayServiceClusterHashKey]
		goalClusterHash, err := generateRayClusterJsonHash(rayServiceInstance.Spec.RayClusterSpec)
		if err != nil {
			errContext := "Failed to serialize new RayCluster config. " +
				"Manual config updates will NOT be tracked accurately. " +
				"Please manually tear down the cluster and apply a new config."
			r.Log.Error(err, errContext)
			return true
		}

		if activeClusterHash != goalClusterHash {
			r.Log.Info("Active RayCluster config doesn't match goal config. " +
				"RayService operator should prepare a new Ray cluster.\n" +
				"* Active RayCluster config hash: " + activeClusterHash + "\n" +
				"* Goal RayCluster config hash: " + goalClusterHash)
		} else {
			r.Log.Info("Active Ray cluster config matches goal config.")
		}

		return activeClusterHash != goalClusterHash
	}

	return false
}

// createRayClusterInstanceIfNeeded checks if we need to create a new RayCluster instance. If so, create one.
func (r *RayServiceReconciler) createRayClusterInstanceIfNeeded(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, pendingRayCluster *rayv1alpha1.RayCluster) (*rayv1alpha1.RayCluster, error) {
	if rayServiceInstance.Status.PendingServiceStatus.RayClusterName == "" {
		// No exist pending RayCluster and no need to create one.
		return nil, nil
	}

	// Create a new RayCluster if:
	// 1. No RayCluster pending.
	// 2. Config update for the pending cluster.
	equal, err := compareRayClusterJsonHash(pendingRayCluster.Spec, rayServiceInstance.Spec.RayClusterSpec)
	if err != nil {
		r.Log.Error(err, "Fail to generate hash for RayClusterSpec")
		return nil, err
	}

	if pendingRayCluster == nil || !equal {
		pendingRayCluster, err = r.createRayClusterInstance(ctx, rayServiceInstance, rayServiceInstance.Status.PendingServiceStatus.RayClusterName)
		if err != nil {
			return nil, err
		}
	}

	return pendingRayCluster, nil
}

// createRayClusterInstance deletes the old RayCluster instance if exists. Only when no existing RayCluster, create a new RayCluster instance.
// One important part is that if this method deletes the old RayCluster, it will return instantly. It depends on the controller to call it again to generate the new RayCluster instance.
func (r *RayServiceReconciler) createRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstanceName string) (*rayv1alpha1.RayCluster, error) {
	r.Log.V(1).Info("createRayClusterInstance", "rayClusterInstanceName", rayClusterInstanceName)

	rayClusterKey := client.ObjectKey{
		Namespace: rayServiceInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1alpha1.RayCluster{}

	var err error
	// Loop until there is no pending RayCluster.
	err = r.Get(ctx, rayClusterKey, rayClusterInstance)

	// If RayCluster exists, it means the config is updated. Delete the previous RayCluster first.
	if err == nil {
		r.Log.V(1).Info("Ray cluster already exists, config changes. Need to recreate. Delete the pending one now.", "key", rayClusterKey.String(), "rayClusterInstance.Spec", rayClusterInstance.Spec, "rayServiceInstance.Spec.RayClusterSpec", rayServiceInstance.Spec.RayClusterSpec)
		delErr := r.Delete(ctx, rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground))
		if delErr == nil {
			// Go to next loop and check if the ray cluster is deleted.
			return nil, nil
		} else if !errors.IsNotFound(delErr) {
			return nil, delErr
		}
		// if error is `not found`, then continue.
	} else if !errors.IsNotFound(err) {
		r.Log.Error(err, "Get request rayCluster instance error!")
		return nil, err
		// if error is `not found`, then continue.
	}

	r.Log.V(1).Info("No pending RayCluster, creating RayCluster.")
	rayClusterInstance, err = r.constructRayClusterForRayService(rayServiceInstance, rayClusterInstanceName)
	if err != nil {
		r.Log.Error(err, "unable to construct rayCluster from spec")
		return nil, err
	}
	if err = r.Create(ctx, rayClusterInstance); err != nil {
		r.Log.Error(err, "unable to create rayCluster for rayService", "rayCluster", rayClusterInstance)
		return nil, err
	}
	r.Log.V(1).Info("created rayCluster for rayService", "rayCluster", rayClusterInstance)

	return rayClusterInstance, nil
}

func (r *RayServiceReconciler) constructRayClusterForRayService(rayService *rayv1alpha1.RayService, rayClusterName string) (*rayv1alpha1.RayCluster, error) {
	var err error
	rayClusterLabel := make(map[string]string)
	for k, v := range rayService.Labels {
		rayClusterLabel[k] = v
	}
	rayClusterLabel[common.RayServiceLabelKey] = rayService.Name
	rayClusterLabel[common.KubernetesCreatedByLabelKey] = common.RayServiceCreatorLabelValue

	rayClusterAnnotations := make(map[string]string)
	for k, v := range rayService.Annotations {
		rayClusterAnnotations[k] = v
	}
	rayClusterAnnotations[common.EnableAgentServiceKey] = common.EnableAgentServiceTrue
	rayClusterAnnotations[common.RayServiceClusterHashKey], err = generateRayClusterJsonHash(rayService.Spec.RayClusterSpec)
	if err != nil {
		errContext := "Failed to serialize RayCluster config. " +
			"Manual config updates will NOT be tracked accurately. " +
			"Please tear down the cluster and apply a new config."
		r.Log.Error(err, errContext)
		return nil, err
	}

	rayCluster := &rayv1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      rayClusterLabel,
			Annotations: rayClusterAnnotations,
			Name:        rayClusterName,
			Namespace:   rayService.Namespace,
		},
		Spec: rayService.Spec.RayClusterSpec,
	}

	// Set the ownership in order to do the garbage collection by k8s.
	if err := ctrl.SetControllerReference(rayService, rayCluster, r.Scheme); err != nil {
		return nil, err
	}

	return rayCluster, nil
}

func (r *RayServiceReconciler) checkIfNeedSubmitServeDeployment(rayServiceInstance *rayv1alpha1.RayService, rayClusterInstance *rayv1alpha1.RayCluster, serveStatus *rayv1alpha1.RayServiceStatus) bool {
	// If the Serve config has not been cached, update the Serve config.
	cacheKey := r.generateConfigKey(rayServiceInstance, rayClusterInstance.Name)
	cachedConfigObj, exist := r.ServeConfigs.Get(cacheKey)

	if !exist {
		r.Log.V(1).Info("shouldUpdate",
			"shouldUpdateServe",
			true,
			"reason",
			fmt.Sprintf(
				"Nothing has been cached for cluster %s with key %s",
				rayClusterInstance.Name,
				cacheKey,
			),
		)
		return true
	}

	// If the Serve config has been cached, check if it needs to be updated.
	shouldUpdate := false
	reason := fmt.Sprintf("Current Serve config matches cached Serve config, "+
		"and some deployments have been deployed for cluster %s", rayClusterInstance.Name)

	serveConfigVersion := r.determineServeConfigVersion(rayServiceInstance)
	if serveConfigVersion == utils.RayServiceServeConfigV1 {
		cachedServeConfig, isServeConfig := cachedConfigObj.(rayv1alpha1.ServeDeploymentGraphSpec)
		if !isServeConfig {
			shouldUpdate = true
			reason = fmt.Sprintf("No Serve config has been cached for cluster %s with key %s", rayClusterInstance.Name, cacheKey)
		} else if !utils.CompareJsonStruct(cachedServeConfig, rayServiceInstance.Spec.ServeDeploymentGraphSpec) {
			shouldUpdate = true
			reason = fmt.Sprintf("Current Serve config doesn't match cached Serve config for cluster %s with key %s", rayClusterInstance.Name, cacheKey)
		}
		r.Log.V(1).Info("shouldUpdate", "shouldUpdateServe", shouldUpdate, "reason", reason, "cachedServeConfig", cachedServeConfig, "current Serve config", rayServiceInstance.Spec.ServeDeploymentGraphSpec)
	} else if serveConfigVersion == utils.RayServiceServeConfigV2 {
		cachedServeConfigV2, isServeConfigV2 := cachedConfigObj.(string)
		if !isServeConfigV2 {
			shouldUpdate = true
			reason = fmt.Sprintf("No Serve config has been cached for cluster %s with key %s", rayClusterInstance.Name, cacheKey)
		} else if cachedServeConfigV2 != rayServiceInstance.Spec.ServeConfigV2 {
			shouldUpdate = true
			reason = fmt.Sprintf("Current Serve config doesn't match cached Serve config for cluster %s with key %s", rayClusterInstance.Name, cacheKey)
		}
		r.Log.V(1).Info("shouldUpdate", "shouldUpdateServe", shouldUpdate, "reason", reason, "cachedServeConfig", cachedServeConfigV2, "current Serve config", rayServiceInstance.Spec.ServeConfigV2)
	}

	return shouldUpdate
}

func (r *RayServiceReconciler) determineServeConfigVersion(rayServiceInstance *rayv1alpha1.RayService) utils.RayServiceServeConfigVersion {
	if rayServiceInstance.Spec.ServeConfigV2 == "" {
		return utils.RayServiceServeConfigV1
	} else {
		return utils.RayServiceServeConfigV2
	}
}

func (r *RayServiceReconciler) updateServeDeployment(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayDashboardClient utils.RayDashboardClientInterface, clusterName string) error {
	serveConfigVersion := r.determineServeConfigVersion(rayServiceInstance)

	if serveConfigVersion == utils.RayServiceServeConfigV1 {
		r.Log.V(1).Info("updateServeDeployment", "V1 config", rayServiceInstance.Spec.ServeDeploymentGraphSpec)

		convertedConfig := rayDashboardClient.ConvertServeConfigV1(rayServiceInstance.Spec.ServeDeploymentGraphSpec)
		configJson, err := json.Marshal(convertedConfig)
		if err != nil {
			return fmt.Errorf("Failed to marshal converted serve config into bytes: %v", err)
		}
		r.Log.V(1).Info("updateServeDeployment", "SINGLE_APP json config", string(configJson))
		if err := rayDashboardClient.UpdateDeployments(ctx, configJson, rayv1alpha1.SINGLE_APP); err != nil {
			r.Log.Error(err, "fail to update deployment")
			return err
		}

		cacheKey := r.generateConfigKey(rayServiceInstance, clusterName)
		r.ServeConfigs.Set(cacheKey, rayServiceInstance.Spec.ServeDeploymentGraphSpec)
		r.Log.V(1).Info("updateServeDeployment", "message", fmt.Sprintf("Cached Serve config for Ray cluster %s with key %s", clusterName, cacheKey))
		return nil
	} else if serveConfigVersion == utils.RayServiceServeConfigV2 {
		r.Log.V(1).Info("updateServeDeployment", "V2 config", rayServiceInstance.Spec.ServeConfigV2)

		serveConfig := make(map[string]interface{})
		if err := yaml.Unmarshal([]byte(rayServiceInstance.Spec.ServeConfigV2), &serveConfig); err != nil {
			return err
		}

		configJson, err := json.Marshal(serveConfig)
		if err != nil {
			return fmt.Errorf("Failed to marshal converted serve config into bytes: %v", err)
		}
		r.Log.V(1).Info("updateServeDeployment", fmt.Sprintf("%s json config", rayServiceInstance.Spec.ServeConfigType), string(configJson))
		if err := rayDashboardClient.UpdateDeployments(ctx, configJson, rayServiceInstance.Spec.ServeConfigType); err != nil {
			r.Log.Error(err, "fail to update deployment")
			return err
		}

		cacheKey := r.generateConfigKey(rayServiceInstance, clusterName)
		r.ServeConfigs.Set(cacheKey, rayServiceInstance.Spec.ServeConfigV2)
		r.Log.V(1).Info("updateServeDeployment", "message", fmt.Sprintf("Cached Serve config for Ray cluster %s with key %s", clusterName, cacheKey))
		return nil
	} else {
		return fmt.Errorf("Serve config version should either be v1 or v2, but got unrecognized serve config version: %s", serveConfigVersion)
	}
}

// getAndCheckServeStatus gets Serve applications' and deployments' statuses,
// updates health timestamps, and checks if the RayCluster is overall healthy.
// It takes as one of its inputs `serveConfigType`, which is used to decide whether
// to query the single-application Serve REST API or the multi-application Serve REST API
// It's return values should be interpreted as
// (Serve app healthy?, Serve app ready?, error if any)
func (r *RayServiceReconciler) getAndCheckServeStatus(ctx context.Context, dashboardClient utils.RayDashboardClientInterface, rayServiceServeStatus *rayv1alpha1.RayServiceStatus, serveConfigType rayv1alpha1.RayServeConfigType, unhealthySecondThreshold *int32) (bool, bool, error) {
	// If the unhealthySecondThreshold value is non-nil, then we will use that value.
	// Otherwise, we will use the value ServiceUnhealthySecondThreshold which can be set in a test
	// This is used for testing purposes.
	serviceUnhealthySecondThreshold := ServiceUnhealthySecondThreshold
	// If serveConfigTypeForTesting is set to a non-nil value by a test, then it will override
	// the serveConfigType value passed into the function. Used for testing purposes
	if serveConfigTypeForTesting != nil {
		serveConfigType = *serveConfigTypeForTesting
	}
	if unhealthySecondThreshold != nil {
		serviceUnhealthySecondThreshold = float64(*unhealthySecondThreshold)
	}

	var serveAppStatuses map[string]*utils.ServeApplicationStatus
	var err error
	if serveConfigType == rayv1alpha1.SINGLE_APP {
		var singleApplicationStatus *utils.ServeApplicationStatus
		if singleApplicationStatus, err = dashboardClient.GetSingleApplicationStatus(ctx); err != nil {
			r.Log.Error(err, "Failed to get Serve deployment statuses from dashboard!")
			return false, false, err
		}
		serveAppStatuses = map[string]*utils.ServeApplicationStatus{common.DefaultServeAppName: singleApplicationStatus}
	} else if serveConfigType == rayv1alpha1.MULTI_APP {
		if serveAppStatuses, err = dashboardClient.GetMultiApplicationStatus(ctx); err != nil {
			r.Log.Error(err, "Failed to get Serve deployment statuses from dashboard!")
			return false, false, err
		}
	} else {
		return false, false, fmt.Errorf("Unrecognized serve config type %s", string(serveConfigType))
	}

	r.Log.V(1).Info("getAndCheckServeStatus", "prev statuses", rayServiceServeStatus.Applications, "serve statuses", serveAppStatuses)

	isHealthy := true
	isReady := true
	timeNow := metav1.Now()

	newApplications := make(map[string]rayv1alpha1.AppStatus)
	for appName, app := range serveAppStatuses {
		if appName == "" {
			appName = common.DefaultServeAppName
		}

		prevApplicationStatus := rayServiceServeStatus.Applications[appName]

		applicationStatus := rayv1alpha1.AppStatus{
			Message:              app.Message,
			Status:               app.Status,
			LastUpdateTime:       &timeNow,
			HealthLastUpdateTime: &timeNow,
			Deployments:          make(map[string]rayv1alpha1.ServeDeploymentStatus),
		}

		// Check app status
		if app.Status != rayv1alpha1.ApplicationStatusEnum.RUNNING {
			// Check previous app status
			if prevApplicationStatus.Status != rayv1alpha1.ApplicationStatusEnum.RUNNING {
				if prevApplicationStatus.HealthLastUpdateTime != nil {
					applicationStatus.HealthLastUpdateTime = prevApplicationStatus.HealthLastUpdateTime

					if time.Since(prevApplicationStatus.HealthLastUpdateTime.Time).Seconds() > serviceUnhealthySecondThreshold {
						isHealthy = false
					}
				}
			}
			isReady = false
		}

		// Check deployment statuses
		for deploymentName, deployment := range app.Deployments {
			deploymentStatus := rayv1alpha1.ServeDeploymentStatus{
				Status:               deployment.Status,
				Message:              deployment.Message,
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			}

			if deployment.Status != rayv1alpha1.DeploymentStatusEnum.HEALTHY {
				prevStatus, exist := prevApplicationStatus.Deployments[deploymentName]
				if exist {
					if prevStatus.Status != rayv1alpha1.DeploymentStatusEnum.HEALTHY {
						deploymentStatus.HealthLastUpdateTime = prevStatus.HealthLastUpdateTime

						if prevStatus.HealthLastUpdateTime != nil && time.Since(prevStatus.HealthLastUpdateTime.Time).Seconds() > serviceUnhealthySecondThreshold {
							isHealthy = false
						}
					}
				}
				isReady = false
			}
			applicationStatus.Deployments[deploymentName] = deploymentStatus
		}
		newApplications[appName] = applicationStatus
	}

	rayServiceServeStatus.Applications = newApplications
	r.Log.V(1).Info("getAndCheckServeStatus", "new statuses", rayServiceServeStatus.Applications)
	return isHealthy, isReady, nil
}

func (r *RayServiceReconciler) generateConfigKey(rayServiceInstance *rayv1alpha1.RayService, clusterName string) string {
	return r.generateConfigKeyPrefix(rayServiceInstance) + clusterName
}

func (r *RayServiceReconciler) generateConfigKeyPrefix(rayServiceInstance *rayv1alpha1.RayService) string {
	return rayServiceInstance.Namespace + "/" + rayServiceInstance.Name + "/"
}

// Return true if healthy, otherwise false.
func (r *RayServiceReconciler) updateAndCheckDashboardStatus(rayServiceClusterStatus *rayv1alpha1.RayServiceStatus, isHealthy bool, unhealthyThreshold *int32) bool {
	timeNow := metav1.Now()
	rayServiceClusterStatus.DashboardStatus.LastUpdateTime = &timeNow
	rayServiceClusterStatus.DashboardStatus.IsHealthy = isHealthy
	if rayServiceClusterStatus.DashboardStatus.HealthLastUpdateTime.IsZero() || isHealthy {
		rayServiceClusterStatus.DashboardStatus.HealthLastUpdateTime = &timeNow
	}

	deploymentUnhealthySecondThreshold := DeploymentUnhealthySecondThreshold
	if unhealthyThreshold != nil {
		deploymentUnhealthySecondThreshold = float64(*unhealthyThreshold)
	}
	return time.Since(rayServiceClusterStatus.DashboardStatus.HealthLastUpdateTime.Time).Seconds() <= deploymentUnhealthySecondThreshold
}

func (r *RayServiceReconciler) markRestart(rayServiceInstance *rayv1alpha1.RayService) {
	// Generate RayCluster name for pending cluster.
	r.Log.V(1).Info("Current cluster is unhealthy, prepare to restart.", "Status", rayServiceInstance.Status)
	rayServiceInstance.Status.ServiceStatus = rayv1alpha1.Restarting
	rayServiceInstance.Status.PendingServiceStatus = rayv1alpha1.RayServiceStatus{
		RayClusterName: utils.GenerateRayClusterName(rayServiceInstance.Name),
	}
}

func (r *RayServiceReconciler) updateRayClusterInfo(rayServiceInstance *rayv1alpha1.RayService, healthyClusterName string) {
	r.Log.V(1).Info("updateRayClusterInfo", "ActiveRayClusterName", rayServiceInstance.Status.ActiveServiceStatus.RayClusterName, "healthyClusterName", healthyClusterName)
	if rayServiceInstance.Status.ActiveServiceStatus.RayClusterName != healthyClusterName {
		rayServiceInstance.Status.ActiveServiceStatus = rayServiceInstance.Status.PendingServiceStatus
		rayServiceInstance.Status.PendingServiceStatus = rayv1alpha1.RayServiceStatus{}
	}
}

// TODO: When start Ingress in RayService, we can disable the Ingress from RayCluster.
func (r *RayServiceReconciler) reconcileIngress(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstance *rayv1alpha1.RayCluster) error {
	if rayClusterInstance.Spec.HeadGroupSpec.EnableIngress == nil || !*rayClusterInstance.Spec.HeadGroupSpec.EnableIngress {
		r.Log.Info("Ingress is disabled. Skipping ingress reconcilation. " +
			"You can enable Ingress by setting enableIngress to true in HeadGroupSpec.")
		return nil
	}

	// Creat Ingress Struct.
	ingress, err := common.BuildIngressForRayService(*rayServiceInstance, *rayClusterInstance)
	if err != nil {
		return err
	}
	ingress.Name = utils.CheckName(ingress.Name)

	// Get Ingress instance.
	headIngress := &networkingv1.Ingress{}
	err = r.Get(ctx, client.ObjectKey{Name: ingress.Name, Namespace: rayServiceInstance.Namespace}, headIngress)

	if err == nil {
		// Update Ingress
		headIngress.Spec = ingress.Spec
		if updateErr := r.Update(ctx, ingress); updateErr != nil {
			r.Log.Error(updateErr, "Ingress Update error!", "Ingress.Error", updateErr)
			return updateErr
		}
	} else if errors.IsNotFound(err) {
		// Create Ingress
		if err := ctrl.SetControllerReference(rayServiceInstance, ingress, r.Scheme); err != nil {
			return err
		}
		if createErr := r.Create(ctx, ingress); createErr != nil {
			if errors.IsAlreadyExists(createErr) {
				r.Log.Info("Ingress already exists,no need to create")
				return nil
			}
			r.Log.Error(createErr, "Ingress create error!", "Ingress.Error", createErr)
			return createErr
		}
	} else {
		r.Log.Error(err, "Ingress get error!")
		return err
	}

	return nil
}

func (r *RayServiceReconciler) reconcileServices(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstance *rayv1alpha1.RayCluster, serviceType common.ServiceType) error {
	r.Log.Info(
		"reconcileServices", "serviceType", serviceType,
		"RayService name", rayServiceInstance.Name, "RayService namespace", rayServiceInstance.Namespace,
	)

	var newSvc *corev1.Service
	var err error

	switch serviceType {
	case common.HeadService:
		newSvc, err = common.BuildHeadServiceForRayService(*rayServiceInstance, *rayClusterInstance)
	case common.ServingService:
		newSvc, err = common.BuildServeServiceForRayService(*rayServiceInstance, *rayClusterInstance)
	default:
		return fmt.Errorf("unknown service type %v", serviceType)
	}

	// TODO: This seems to be useless because BuildHeadServiceForRayService and BuildServeServiceForRayService will not return error.
	if err != nil {
		return err
	}
	r.Log.Info("reconcileServices", "newSvc", newSvc)

	// Retrieve the Service from the Kubernetes cluster with the name and namespace.
	oldSvc := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Name: newSvc.Name, Namespace: rayServiceInstance.Namespace}, oldSvc)

	if err == nil {
		// Only update the service if the RayCluster switches.
		if newSvc.Spec.Selector[common.RayClusterLabelKey] == oldSvc.Spec.Selector[common.RayClusterLabelKey] {
			r.Log.Info(fmt.Sprintf("RayCluster %v's %v has already exists, skip Update", newSvc.Spec.Selector[common.RayClusterLabelKey], serviceType))
			return nil
		}

		// ClusterIP is immutable. Starting from Kubernetes v1.21.5, if the new service does not specify a ClusterIP,
		// Kubernetes will assign the ClusterIP of the old service to the new one. However, to maintain compatibility
		// with older versions of Kubernetes, we need to assign the ClusterIP here.
		if newSvc.Spec.ClusterIP == "" {
			newSvc.Spec.ClusterIP = oldSvc.Spec.ClusterIP
		}

		// TODO (kevin85421): Consider not only the updates of the Spec but also the ObjectMeta.
		oldSvc.Spec = *newSvc.Spec.DeepCopy()
		r.Log.Info(fmt.Sprintf("Update Kubernetes Service serviceType %v", serviceType))
		if updateErr := r.Update(ctx, oldSvc); updateErr != nil {
			r.Log.Error(updateErr, fmt.Sprintf("Fail to update Kubernetes Service serviceType %v", serviceType), "Error", updateErr)
			return updateErr
		}
	} else if errors.IsNotFound(err) {
		r.Log.Info(fmt.Sprintf("Create a Kubernetes Service for RayService serviceType %v", serviceType))
		if err := ctrl.SetControllerReference(rayServiceInstance, newSvc, r.Scheme); err != nil {
			return err
		}
		if createErr := r.Create(ctx, newSvc); createErr != nil {
			if errors.IsAlreadyExists(createErr) {
				r.Log.Info("The Kubernetes Service already exists, no need to create.")
				return nil
			}
			r.Log.Error(createErr, fmt.Sprintf("Fail to create Kubernetes Service serviceType %v", serviceType), "Error", createErr)
			return createErr
		}
	} else {
		r.Log.Error(err, "Fail to retrieve the Kubernetes Service from the cluster!")
		return err
	}

	return nil
}

func (r *RayServiceReconciler) updateStatusForActiveCluster(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstance *rayv1alpha1.RayCluster, logger logr.Logger) error {
	rayServiceInstance.Status.ActiveServiceStatus.RayClusterStatus = rayClusterInstance.Status

	var err error
	var clientURL string
	rayServiceStatus := &rayServiceInstance.Status.ActiveServiceStatus

	if clientURL, err = utils.FetchDashboardAgentURL(ctx, &r.Log, r.Client, rayClusterInstance); err != nil || clientURL == "" {
		r.updateAndCheckDashboardStatus(rayServiceStatus, false, rayServiceInstance.Spec.DeploymentUnhealthySecondThreshold)
		return err
	}

	rayDashboardClient := utils.GetRayDashboardClientFunc()
	rayDashboardClient.InitClient(clientURL)

	var isHealthy, isReady bool
	if isHealthy, isReady, err = r.getAndCheckServeStatus(ctx, rayDashboardClient, rayServiceStatus, rayServiceInstance.Spec.ServeConfigType, rayServiceInstance.Spec.ServiceUnhealthySecondThreshold); err != nil {
		r.updateAndCheckDashboardStatus(rayServiceStatus, false, rayServiceInstance.Spec.DeploymentUnhealthySecondThreshold)
		return err
	}

	r.updateAndCheckDashboardStatus(rayServiceStatus, true, rayServiceInstance.Spec.DeploymentUnhealthySecondThreshold)

	logger.Info("Check serve health", "isHealthy", isHealthy, "isReady", isReady)

	return err
}

// Reconciles the Serve app on the rayClusterInstance. Returns
// (ctrl.Result, isHealthy, isReady, error). isHealthy indicates whether the
// Serve app is behaving as expected. isReady indicates whether the Serve app
// (including all deployments) is running. E.g. while the Serve app is
// deploying/updating, isHealthy is true while isReady is false. If isHealthy
// is false, isReady is guaranteed to be false.
func (r *RayServiceReconciler) reconcileServe(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstance *rayv1alpha1.RayCluster, isActive bool, logger logr.Logger) (ctrl.Result, bool, bool, error) {
	rayServiceInstance.Status.ActiveServiceStatus.RayClusterStatus = rayClusterInstance.Status
	var err error
	var clientURL string
	var rayServiceStatus *rayv1alpha1.RayServiceStatus

	// Pick up service status to be updated.
	if isActive {
		rayServiceStatus = &rayServiceInstance.Status.ActiveServiceStatus
	} else {
		rayServiceStatus = &rayServiceInstance.Status.PendingServiceStatus
	}

	if clientURL, err = utils.FetchDashboardAgentURL(ctx, &r.Log, r.Client, rayClusterInstance); err != nil || clientURL == "" {
		if !r.updateAndCheckDashboardStatus(rayServiceStatus, false, rayServiceInstance.Spec.DeploymentUnhealthySecondThreshold) {
			logger.Info("Dashboard is unhealthy, restart the cluster.")
			r.markRestart(rayServiceInstance)
		}
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.WaitForDashboard, err)
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, false, err
	}

	// Check if head pod is running and ready. If not, requeue the resource event to avoid
	// redundant custom resource status updates.
	//
	// TODO (kevin85421): Note that the Dashboard and GCS may take a few seconds to start up
	// after the head pod is running and ready. Hence, some requests to the Dashboard (e.g. `UpdateDeployments`) may fail.
	// This is not an issue since `UpdateDeployments` is an idempotent operation.
	if isRunningAndReady, err := r.isHeadPodRunningAndReady(ctx, rayClusterInstance); err != nil || !isRunningAndReady {
		if err != nil {
			logger.Error(err, "Failed to check if head pod is running and ready!")
		} else {
			logger.Info("Skipping the update of Serve deployments because the Ray head pod is not ready.")
		}
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, false, err
	}

	rayDashboardClient := utils.GetRayDashboardClientFunc()
	rayDashboardClient.InitClient(clientURL)

	shouldUpdate := r.checkIfNeedSubmitServeDeployment(rayServiceInstance, rayClusterInstance, rayServiceStatus)

	if shouldUpdate {
		if err = r.updateServeDeployment(ctx, rayServiceInstance, rayDashboardClient, rayClusterInstance.Name); err != nil {
			if !r.updateAndCheckDashboardStatus(rayServiceStatus, false, rayServiceInstance.Spec.DeploymentUnhealthySecondThreshold) {
				logger.Info("Dashboard is unhealthy, restart the cluster.")
				r.markRestart(rayServiceInstance)
			}
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.WaitForServeDeploymentReady, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, false, err
		}

		r.Recorder.Eventf(rayServiceInstance, "Normal", "SubmittedServeDeployment",
			"Controller sent API request to update Serve deployments on cluster %s", rayClusterInstance.Name)
	}

	var isHealthy, isReady bool
	if isHealthy, isReady, err = r.getAndCheckServeStatus(ctx, rayDashboardClient, rayServiceStatus, rayServiceInstance.Spec.ServeConfigType, rayServiceInstance.Spec.DeploymentUnhealthySecondThreshold); err != nil {
		if !r.updateAndCheckDashboardStatus(rayServiceStatus, false, rayServiceInstance.Spec.DeploymentUnhealthySecondThreshold) {
			logger.Info("Dashboard is unhealthy, restart the cluster.")
			r.markRestart(rayServiceInstance)
		}
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailedToGetServeDeploymentStatus, err)
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, false, err
	}

	r.updateAndCheckDashboardStatus(rayServiceStatus, true, rayServiceInstance.Spec.DeploymentUnhealthySecondThreshold)

	logger.Info("Check serve health", "isHealthy", isHealthy, "isReady", isReady, "isActive", isActive)

	if isHealthy && isReady {
		rayServiceInstance.Status.ServiceStatus = rayv1alpha1.Running
		r.updateRayClusterInfo(rayServiceInstance, rayClusterInstance.Name)
		r.Recorder.Event(rayServiceInstance, "Normal", "Running", "The Serve applicaton is now running and healthy.")
	} else if isHealthy && !isReady {
		rayServiceInstance.Status.ServiceStatus = rayv1alpha1.WaitForServeDeploymentReady
		if err := r.Status().Update(ctx, rayServiceInstance); err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, true, false, err
		}
		logger.Info("Mark cluster as waiting for Serve deployments", "rayCluster", rayClusterInstance)
	} else if !isHealthy {
		// NOTE: When isHealthy is false, isReady is guaranteed to be false.
		r.markRestart(rayServiceInstance)
		rayServiceInstance.Status.ServiceStatus = rayv1alpha1.Restarting
		if err := r.Status().Update(ctx, rayServiceInstance); err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, false, err
		}

		logger.Info("Mark cluster as unhealthy", "rayCluster", rayClusterInstance)
		r.Recorder.Eventf(rayServiceInstance, "Normal", "Restarting", "The cluster will restart after %s", ServiceRestartRequeueDuration)
		// Wait a while for the cluster delete
		return ctrl.Result{RequeueAfter: ServiceRestartRequeueDuration}, false, false, nil
	}

	return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, isHealthy, isReady, nil
}

func (r *RayServiceReconciler) labelHealthyServePods(ctx context.Context, rayClusterInstance *rayv1alpha1.RayCluster) error {
	allPods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: rayClusterInstance.Name}

	if err := r.List(ctx, &allPods, client.InNamespace(rayClusterInstance.Namespace), filterLabels); err != nil {
		return err
	}

	httpProxyClient := utils.GetRayHttpProxyClientFunc()
	httpProxyClient.InitClient()
	for _, pod := range allPods.Items {
		rayContainer := pod.Spec.Containers[utils.FindRayContainerIndex(pod.Spec)]
		servingPort := utils.FindContainerPort(&rayContainer, common.DefaultServingPortName, common.DefaultServingPort)
		httpProxyClient.SetHostIp(pod.Status.PodIP, servingPort)
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		if httpProxyClient.CheckHealth() == nil {
			pod.Labels[common.RayClusterServingServiceLabelKey] = common.EnableRayClusterServingServiceTrue
		} else {
			pod.Labels[common.RayClusterServingServiceLabelKey] = common.EnableRayClusterServingServiceFalse
		}
		if updateErr := r.Update(ctx, &pod); updateErr != nil {
			r.Log.Error(updateErr, "Pod label Update error!", "Pod.Error", updateErr)
			return updateErr
		}
	}

	return nil
}

func generateRayClusterJsonHash(rayClusterSpec rayv1alpha1.RayClusterSpec) (string, error) {
	// Mute all fields that will not trigger new RayCluster preparation. For example,
	// Autoscaler will update `Replicas` and `WorkersToDelete` when scaling up/down.
	updatedRayClusterSpec := rayClusterSpec.DeepCopy()
	for i := 0; i < len(updatedRayClusterSpec.WorkerGroupSpecs); i++ {
		updatedRayClusterSpec.WorkerGroupSpecs[i].Replicas = nil
		updatedRayClusterSpec.WorkerGroupSpecs[i].ScaleStrategy.WorkersToDelete = nil
	}

	// Generate a hash for the RayClusterSpec.
	return utils.GenerateJsonHash(updatedRayClusterSpec)
}

func compareRayClusterJsonHash(spec1 rayv1alpha1.RayClusterSpec, spec2 rayv1alpha1.RayClusterSpec) (bool, error) {
	hash1, err1 := generateRayClusterJsonHash(spec1)
	if err1 != nil {
		return false, err1
	}

	hash2, err2 := generateRayClusterJsonHash(spec2)
	if err2 != nil {
		return false, err2
	}
	return hash1 == hash2, nil
}

// isHeadPodRunningAndReady checks if the head pod of the RayCluster is running and ready.
func (r *RayServiceReconciler) isHeadPodRunningAndReady(ctx context.Context, instance *rayv1alpha1.RayCluster) (bool, error) {
	podList := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode)}

	if err := r.List(ctx, &podList, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		r.Log.Error(err, "Failed to list the head Pod of the RayCluster %s in the namespace %s", instance.Name, instance.Namespace)
		return false, err
	}

	if len(podList.Items) != 1 {
		return false, fmt.Errorf("Found %d head pods for RayCluster %s in the namespace %s", len(podList.Items), instance.Name, instance.Namespace)
	}

	return utils.IsRunningAndReady(&podList.Items[0]), nil
}
