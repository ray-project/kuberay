package ray

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

	cmap "github.com/orcaman/concurrent-map/v2"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

const (
	ServiceDefaultRequeueDuration   = 2 * time.Second
	RayClusterDeletionDelayDuration = 60 * time.Second
	ENABLE_ZERO_DOWNTIME            = "ENABLE_ZERO_DOWNTIME"
)

// RayServiceReconciler reconciles a RayService object
type RayServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// Currently, the Ray dashboard doesn't cache the Serve deployment config.
	// To avoid reapplying the same config repeatedly, cache the config in this map.
	ServeConfigs                 cmap.ConcurrentMap[string, string]
	RayClusterDeletionTimestamps cmap.ConcurrentMap[string, time.Time]

	dashboardClientFunc func() utils.RayDashboardClientInterface
	httpProxyClientFunc func() utils.RayHttpProxyClientInterface
}

// NewRayServiceReconciler returns a new reconcile.Reconciler
func NewRayServiceReconciler(_ context.Context, mgr manager.Manager, provider utils.ClientProvider) *RayServiceReconciler {
	dashboardClientFunc := provider.GetDashboardClient(mgr)
	httpProxyClientFunc := provider.GetHttpProxyClient(mgr)
	return &RayServiceReconciler{
		Client:                       mgr.GetClient(),
		Scheme:                       mgr.GetScheme(),
		Recorder:                     mgr.GetEventRecorderFor("rayservice-controller"),
		ServeConfigs:                 cmap.New[string](),
		RayClusterDeletionTimestamps: cmap.New[time.Time](),

		dashboardClientFunc: dashboardClientFunc,
		httpProxyClientFunc: httpProxyClientFunc,
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
// +kubebuilder:rbac:groups=core,resources=pods/proxy,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=services/proxy,verbs=get;update;patch
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
	logger := ctrl.LoggerFrom(ctx)

	isReady := false

	var rayServiceInstance *rayv1.RayService
	var err error
	var ctrlResult ctrl.Result

	// Resolve the CR from request.
	if rayServiceInstance, err = r.getRayServiceInstance(ctx, request); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	originalRayServiceInstance := rayServiceInstance.DeepCopy()
	r.cleanUpServeConfigCache(ctx, rayServiceInstance)

	// TODO (kevin85421): ObservedGeneration should be used to determine whether to update this CR or not.
	rayServiceInstance.Status.ObservedGeneration = rayServiceInstance.ObjectMeta.Generation

	// Find active and pending ray cluster objects given current service name.
	var activeRayClusterInstance *rayv1.RayCluster
	var pendingRayClusterInstance *rayv1.RayCluster
	if activeRayClusterInstance, pendingRayClusterInstance, err = r.reconcileRayCluster(ctx, rayServiceInstance); err != nil {
		err = r.updateState(ctx, rayServiceInstance, rayv1.FailedToGetOrCreateRayCluster, err)
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
		rayServiceInstance.Status.PendingServiceStatus = rayv1.RayServiceStatus{}
		if ctrlResult, isReady, err = r.reconcileServe(ctx, rayServiceInstance, activeRayClusterInstance, true); err != nil {
			logger.Error(err, "Fail to reconcileServe.")
			return ctrlResult, nil
		}
	} else if activeRayClusterInstance != nil && pendingRayClusterInstance != nil {
		logger.Info("Reconciling the Serve component. Active and pending Ray clusters exist.")
		// TODO (kevin85421): This can most likely be removed.
		if err = r.updateStatusForActiveCluster(ctx, rayServiceInstance, activeRayClusterInstance); err != nil {
			logger.Error(err, "Failed to update active Ray cluster's status.")
		}

		if ctrlResult, isReady, err = r.reconcileServe(ctx, rayServiceInstance, pendingRayClusterInstance, false); err != nil {
			logger.Error(err, "Fail to reconcileServe.")
			return ctrlResult, nil
		}
	} else if activeRayClusterInstance == nil && pendingRayClusterInstance != nil {
		rayServiceInstance.Status.ActiveServiceStatus = rayv1.RayServiceStatus{}
		if ctrlResult, isReady, err = r.reconcileServe(ctx, rayServiceInstance, pendingRayClusterInstance, false); err != nil {
			logger.Error(err, "Fail to reconcileServe.")
			return ctrlResult, nil
		}
	} else {
		logger.Info("Reconciling the Serve component. No Ray cluster exists.")
		rayServiceInstance.Status.ActiveServiceStatus = rayv1.RayServiceStatus{}
		rayServiceInstance.Status.PendingServiceStatus = rayv1.RayServiceStatus{}
	}

	if !isReady {
		logger.Info(fmt.Sprintf("Ray Serve applications are not ready to serve requests: checking again in %ss", ServiceDefaultRequeueDuration))
		r.Recorder.Eventf(rayServiceInstance, "Normal", "ServiceNotReady", "The service is not ready yet. Controller will perform a round of actions in %s.", ServiceDefaultRequeueDuration)
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, nil
	}

	// Get the ready Ray cluster instance for service and ingress update.
	var rayClusterInstance *rayv1.RayCluster
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
		if err := r.reconcileServices(ctx, rayServiceInstance, rayClusterInstance, utils.HeadService); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1.FailedToUpdateService, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
		if err := r.labelHeadPodForServeStatus(ctx, rayClusterInstance); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1.FailedToUpdateServingPodLabel, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
		if err := r.reconcileServices(ctx, rayServiceInstance, rayClusterInstance, utils.ServingService); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1.FailedToUpdateService, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
	}

	if err := r.calculateStatus(ctx, rayServiceInstance); err != nil {
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
	}

	// Final status update for any CR modification.
	if r.inconsistentRayServiceStatuses(ctx, originalRayServiceInstance.Status, rayServiceInstance.Status) {
		rayServiceInstance.Status.LastUpdateTime = &metav1.Time{Time: time.Now()}
		if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
			logger.Error(errStatus, "Failed to update RayService status", "rayServiceInstance", rayServiceInstance)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, errStatus
		}
	}

	return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, nil
}

func (r *RayServiceReconciler) calculateStatus(ctx context.Context, rayServiceInstance *rayv1.RayService) error {
	serveEndPoints := &corev1.Endpoints{}
	if err := r.Get(ctx, common.RayServiceServeServiceNamespacedName(rayServiceInstance), serveEndPoints); err != nil && !errors.IsNotFound(err) {
		return err
	}

	numServeEndpoints := 0
	// Ray Pod addresses are categorized into subsets based on the IPs they share.
	// subset.Addresses contains a list of Ray Pod addresses with ready serve port.
	for _, subset := range serveEndPoints.Subsets {
		numServeEndpoints += len(subset.Addresses)
	}
	rayServiceInstance.Status.NumServeEndpoints = int32(numServeEndpoints)
	return nil
}

// Checks whether the old and new RayServiceStatus are inconsistent by comparing different fields.
// If the only difference between the old and new status is the HealthLastUpdateTime field,
// the status update will not be triggered.
// The RayClusterStatus field is only for observability in RayService CR, and changes to it will not trigger the status update.
func (r *RayServiceReconciler) inconsistentRayServiceStatus(ctx context.Context, oldStatus rayv1.RayServiceStatus, newStatus rayv1.RayServiceStatus) bool {
	logger := ctrl.LoggerFrom(ctx)
	if oldStatus.RayClusterName != newStatus.RayClusterName {
		logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService RayClusterName changed from %s to %s", oldStatus.RayClusterName, newStatus.RayClusterName))
		return true
	}

	if len(oldStatus.Applications) != len(newStatus.Applications) {
		return true
	}

	var ok bool
	for appName, newAppStatus := range newStatus.Applications {
		var oldAppStatus rayv1.AppStatus
		if oldAppStatus, ok = oldStatus.Applications[appName]; !ok {
			logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService new application %s found", appName))
			return true
		}

		if oldAppStatus.Status != newAppStatus.Status {
			logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService application %s status changed from %v to %v", appName, oldAppStatus.Status, newAppStatus.Status))
			return true
		} else if oldAppStatus.Message != newAppStatus.Message {
			logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService application %s status message changed from %v to %v", appName, oldAppStatus.Message, newAppStatus.Message))
			return true
		}

		if len(oldAppStatus.Deployments) != len(newAppStatus.Deployments) {
			return true
		}

		for deploymentName, newDeploymentStatus := range newAppStatus.Deployments {
			var oldDeploymentStatus rayv1.ServeDeploymentStatus
			if oldDeploymentStatus, ok = oldAppStatus.Deployments[deploymentName]; !ok {
				logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService new deployment %s found in application %s", deploymentName, appName))
				return true
			}

			if oldDeploymentStatus.Status != newDeploymentStatus.Status {
				logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService DeploymentStatus changed from %v to %v", oldDeploymentStatus.Status, newDeploymentStatus.Status))
				return true
			} else if oldDeploymentStatus.Message != newDeploymentStatus.Message {
				logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService deployment status message changed from %v to %v", oldDeploymentStatus.Message, newDeploymentStatus.Message))
				return true
			}
		}
	}

	return false
}

// Determine whether to update the status of the RayService instance.
func (r *RayServiceReconciler) inconsistentRayServiceStatuses(ctx context.Context, oldStatus rayv1.RayServiceStatuses, newStatus rayv1.RayServiceStatuses) bool {
	logger := ctrl.LoggerFrom(ctx)
	if oldStatus.ServiceStatus != newStatus.ServiceStatus {
		logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService ServiceStatus changed from %s to %s", oldStatus.ServiceStatus, newStatus.ServiceStatus))
		return true
	}

	if oldStatus.NumServeEndpoints != newStatus.NumServeEndpoints {
		logger.Info(fmt.Sprintf("inconsistentRayServiceStatus RayService NumServeEndpoints changed from %d to %d", oldStatus.NumServeEndpoints, newStatus.NumServeEndpoints))
		return true
	}

	if r.inconsistentRayServiceStatus(ctx, oldStatus.ActiveServiceStatus, newStatus.ActiveServiceStatus) {
		logger.Info("inconsistentRayServiceStatus RayService ActiveServiceStatus changed")
		return true
	}

	if r.inconsistentRayServiceStatus(ctx, oldStatus.PendingServiceStatus, newStatus.PendingServiceStatus) {
		logger.Info("inconsistentRayServiceStatus RayService PendingServiceStatus changed")
		return true
	}

	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayService{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		Owns(&rayv1.RayCluster{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		WithOptions(controller.Options{
			LogConstructor: func(request *reconcile.Request) logr.Logger {
				logger := ctrl.Log.WithName("controllers").WithName("RayService")
				if request != nil {
					logger = logger.WithValues("RayService", request.NamespacedName)
				}
				return logger
			},
		}).
		Complete(r)
}

func (r *RayServiceReconciler) getRayServiceInstance(ctx context.Context, request ctrl.Request) (*rayv1.RayService, error) {
	logger := ctrl.LoggerFrom(ctx)
	rayServiceInstance := &rayv1.RayService{}
	if err := r.Get(ctx, request.NamespacedName, rayServiceInstance); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Read request instance not found error!")
		} else {
			logger.Error(err, "Read request instance error!")
		}
		return nil, err
	}
	return rayServiceInstance, nil
}

func (r *RayServiceReconciler) updateState(ctx context.Context, rayServiceInstance *rayv1.RayService, status rayv1.ServiceStatus, err error) error {
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
func (r *RayServiceReconciler) reconcileRayCluster(ctx context.Context, rayServiceInstance *rayv1.RayService) (*rayv1.RayCluster, *rayv1.RayCluster, error) {
	logger := ctrl.LoggerFrom(ctx)
	var err error
	if err = r.cleanUpRayClusterInstance(ctx, rayServiceInstance); err != nil {
		return nil, nil, err
	}

	// Get active cluster and pending cluster instances.
	activeRayCluster, err := r.getRayClusterByNamespacedName(ctx, common.RayServiceActiveRayClusterNamespacedName(rayServiceInstance))
	if err != nil {
		return nil, nil, err
	}

	pendingRayCluster, err := r.getRayClusterByNamespacedName(ctx, common.RayServicePendingRayClusterNamespacedName(rayServiceInstance))
	if err != nil {
		return nil, nil, err
	}

	clusterAction := r.shouldPrepareNewRayCluster(ctx, rayServiceInstance, activeRayCluster)
	if clusterAction == RolloutNew {
		// For LLM serving, some users might not have sufficient GPU resources to run two RayClusters simultaneously.
		// Therefore, KubeRay offers ENABLE_ZERO_DOWNTIME as a feature flag for zero-downtime upgrades.
		enableZeroDowntime := true
		if s := os.Getenv(ENABLE_ZERO_DOWNTIME); strings.ToLower(s) == "false" {
			enableZeroDowntime = false
		}
		if enableZeroDowntime || !enableZeroDowntime && activeRayCluster == nil {
			// Add a pending cluster name. In the next reconcile loop, shouldPrepareNewRayCluster will return DoNothing and we will
			// actually create the pending RayCluster instance.
			r.markRestartAndAddPendingClusterName(ctx, rayServiceInstance)
		} else {
			logger.Info("Zero-downtime upgrade is disabled (ENABLE_ZERO_DOWNTIME: false). Skip preparing a new RayCluster.")
		}
		return activeRayCluster, nil, nil
	} else if clusterAction == Update {
		// Update the active cluster.
		logger.Info("Updating the active RayCluster instance.")
		if activeRayCluster, err = r.constructRayClusterForRayService(ctx, rayServiceInstance, activeRayCluster.Name); err != nil {
			return nil, nil, err
		}
		if err := r.updateRayClusterInstance(ctx, activeRayCluster); err != nil {
			return nil, nil, err
		}
		return activeRayCluster, nil, nil
	}

	if pendingRayCluster, err = r.createRayClusterInstanceIfNeeded(ctx, rayServiceInstance, pendingRayCluster); err != nil {
		return nil, nil, err
	}

	return activeRayCluster, pendingRayCluster, nil
}

// cleanUpRayClusterInstance cleans up all the dangling RayCluster instances that are owned by the RayService instance.
func (r *RayServiceReconciler) cleanUpRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1.RayService) error {
	logger := ctrl.LoggerFrom(ctx)
	rayClusterList := rayv1.RayClusterList{}

	var err error
	if err = r.List(ctx, &rayClusterList, common.RayServiceRayClustersAssociationOptions(rayServiceInstance).ToListOptions()...); err != nil {
		return err
	}

	// Clean up RayCluster instances. Each instance is deleted 60 seconds
	// after becoming inactive to give the ingress time to update.
	for _, rayClusterInstance := range rayClusterList.Items {
		if rayClusterInstance.Name != rayServiceInstance.Status.ActiveServiceStatus.RayClusterName && rayClusterInstance.Name != rayServiceInstance.Status.PendingServiceStatus.RayClusterName {
			cachedTimestamp, exists := r.RayClusterDeletionTimestamps.Get(rayClusterInstance.Name)
			if !exists {
				deletionTimestamp := metav1.Now().Add(RayClusterDeletionDelayDuration)
				r.RayClusterDeletionTimestamps.Set(rayClusterInstance.Name, deletionTimestamp)
				logger.Info(fmt.Sprintf("Scheduled dangling RayCluster "+
					"%s for deletion at %s", rayClusterInstance.Name, deletionTimestamp))
			} else {
				reasonForDeletion := ""
				if time.Since(cachedTimestamp) > 0*time.Second {
					reasonForDeletion = fmt.Sprintf("Deletion timestamp %s "+
						"for RayCluster %s has passed. Deleting cluster "+
						"immediately.", cachedTimestamp, rayClusterInstance.Name)
				}

				if reasonForDeletion != "" {
					logger.Info("reconcileRayCluster", "delete Ray cluster", rayClusterInstance.Name, "reason", reasonForDeletion)
					if err := r.Delete(ctx, &rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (r *RayServiceReconciler) getRayClusterByNamespacedName(ctx context.Context, clusterKey client.ObjectKey) (*rayv1.RayCluster, error) {
	rayCluster := &rayv1.RayCluster{}
	if clusterKey.Name != "" {
		// Ignore not found since in that case we should return RayCluster as nil.
		if err := r.Get(ctx, clusterKey, rayCluster); client.IgnoreNotFound(err) != nil {
			return nil, err
		}
	} else {
		rayCluster = nil
	}

	return rayCluster, nil
}

// cleanUpServeConfigCache cleans up the unused serve deployments config in the cached map.
func (r *RayServiceReconciler) cleanUpServeConfigCache(ctx context.Context, rayServiceInstance *rayv1.RayService) {
	logger := ctrl.LoggerFrom(ctx)
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
		logger.Info("cleanUpServeConfigCache", "activeConfigKey", activeConfigKey, "pendingConfigKey", pendingConfigKey, "remove key", key)
		r.ServeConfigs.Remove(key)
	}
}

type ClusterAction int

const (
	DoNothing  ClusterAction = iota // value 0
	Update                          // value 1
	RolloutNew                      // value 2
)

// shouldPrepareNewRayCluster checks if we need to generate a new pending cluster.
func (r *RayServiceReconciler) shouldPrepareNewRayCluster(ctx context.Context, rayServiceInstance *rayv1.RayService, activeRayCluster *rayv1.RayCluster) ClusterAction {
	logger := ctrl.LoggerFrom(ctx)
	// Prepare new RayCluster if:
	// 1. No active cluster and no pending cluster
	// 2. No pending cluster, and the active RayCluster has changed.
	if rayServiceInstance.Status.PendingServiceStatus.RayClusterName == "" {
		if activeRayCluster == nil {
			logger.Info("No active Ray cluster. RayService operator should prepare a new Ray cluster.")
			return RolloutNew
		}

		// Case 1: If everything is identical except for the Replicas and WorkersToDelete of
		// each WorkerGroup, then do nothing.
		activeClusterHash := activeRayCluster.ObjectMeta.Annotations[utils.HashWithoutReplicasAndWorkersToDeleteKey]
		goalClusterHash, err := generateHashWithoutReplicasAndWorkersToDelete(rayServiceInstance.Spec.RayClusterSpec)
		errContextFailedToSerialize := "Failed to serialize new RayCluster config. " +
			"Manual config updates will NOT be tracked accurately. " +
			"Please manually tear down the cluster and apply a new config."
		if err != nil {
			logger.Error(err, errContextFailedToSerialize)
			return DoNothing
		}

		if activeClusterHash == goalClusterHash {
			logger.Info("Active Ray cluster config matches goal config. No need to update RayCluster.")
			return DoNothing
		}

		// Case 2: Otherwise, if everything is identical except for the Replicas and WorkersToDelete of
		// the existing workergroups, and one or more new workergroups are added at the end, then update the cluster.
		activeClusterNumWorkerGroups, err := strconv.Atoi(activeRayCluster.ObjectMeta.Annotations[utils.NumWorkerGroupsKey])
		if err != nil {
			logger.Error(err, errContextFailedToSerialize)
			return DoNothing
		}
		goalNumWorkerGroups := len(rayServiceInstance.Spec.RayClusterSpec.WorkerGroupSpecs)
		logger.Info("number of worker groups", "activeClusterNumWorkerGroups", activeClusterNumWorkerGroups, "goalNumWorkerGroups", goalNumWorkerGroups)
		if goalNumWorkerGroups > activeClusterNumWorkerGroups {

			// Remove the new workergroup(s) from the end before calculating the hash.
			goalClusterSpec := rayServiceInstance.Spec.RayClusterSpec.DeepCopy()
			goalClusterSpec.WorkerGroupSpecs = goalClusterSpec.WorkerGroupSpecs[:activeClusterNumWorkerGroups]

			// Generate the hash of the old worker group specs.
			goalClusterHash, err = generateHashWithoutReplicasAndWorkersToDelete(*goalClusterSpec)
			if err != nil {
				logger.Error(err, errContextFailedToSerialize)
				return DoNothing
			}

			if activeClusterHash == goalClusterHash {
				logger.Info("Active RayCluster config matches goal config, except that one or more entries were appended to WorkerGroupSpecs. Updating RayCluster.")
				return Update
			}
		}

		// Case 3: Otherwise, rollout a new cluster.
		logger.Info("Active RayCluster config doesn't match goal config. " +
			"RayService operator should prepare a new Ray cluster.\n" +
			"* Active RayCluster config hash: " + activeClusterHash + "\n" +
			"* Goal RayCluster config hash: " + goalClusterHash)
		return RolloutNew
	}

	return DoNothing
}

// createRayClusterInstanceIfNeeded checks if we need to create a new RayCluster instance. If so, create one.
func (r *RayServiceReconciler) createRayClusterInstanceIfNeeded(ctx context.Context, rayServiceInstance *rayv1.RayService, pendingRayCluster *rayv1.RayCluster) (*rayv1.RayCluster, error) {
	logger := ctrl.LoggerFrom(ctx)
	// Early return if no pending RayCluster needs to be created.
	if rayServiceInstance.Status.PendingServiceStatus.RayClusterName == "" {
		return nil, nil
	}

	var clusterAction ClusterAction
	var err error

	if pendingRayCluster == nil {
		clusterAction = RolloutNew
	} else {
		clusterAction, err = getClusterAction(pendingRayCluster.Spec, rayServiceInstance.Spec.RayClusterSpec)
		if err != nil {
			err = fmt.Errorf("Fail to generate hash for RayClusterSpec: %w", err)
			return nil, err
		}
	}

	switch clusterAction {
	case RolloutNew:
		logger.Info("Creating a new pending RayCluster instance.")
		pendingRayCluster, err = r.createRayClusterInstance(ctx, rayServiceInstance)
	case Update:
		logger.Info("Updating the pending RayCluster instance.")
		if pendingRayCluster, err = r.constructRayClusterForRayService(ctx, rayServiceInstance, pendingRayCluster.Name); err != nil {
			return nil, err
		}
		err = r.updateRayClusterInstance(ctx, pendingRayCluster)
	}

	if err != nil {
		return nil, err
	}

	return pendingRayCluster, nil
}

// updateRayClusterInstance updates the RayCluster instance.
func (r *RayServiceReconciler) updateRayClusterInstance(ctx context.Context, rayClusterInstance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("updateRayClusterInstance", "Name", rayClusterInstance.Name, "Namespace", rayClusterInstance.Namespace)
	// Printing the whole RayCluster is too noisy. Only print the spec.
	logger.Info("updateRayClusterInstance", "rayClusterInstance.Spec", rayClusterInstance.Spec)

	// Fetch the current state of the RayCluster
	currentRayCluster, err := r.getRayClusterByNamespacedName(ctx, client.ObjectKey{
		Namespace: rayClusterInstance.Namespace,
		Name:      rayClusterInstance.Name,
	})
	if err != nil {
		err = fmt.Errorf("Failed to get the current state of RayCluster, namespace: %s, name: %s: %w", rayClusterInstance.Namespace, rayClusterInstance.Name, err)
		return err
	}

	if currentRayCluster == nil {
		logger.Info("RayCluster not found, possibly deleted", "Namespace", rayClusterInstance.Namespace, "Name", rayClusterInstance.Name)
		return nil
	}

	// Update the fetched RayCluster with new changes
	currentRayCluster.Spec = rayClusterInstance.Spec

	// Update the labels and annotations
	currentRayCluster.Labels = rayClusterInstance.Labels
	currentRayCluster.Annotations = rayClusterInstance.Annotations

	// Update the RayCluster
	if err = r.Update(ctx, currentRayCluster); err != nil {
		return err
	}

	logger.Info("updated RayCluster", "rayClusterInstance", currentRayCluster)
	return nil
}

// createRayClusterInstance deletes the old RayCluster instance if exists. Only when no existing RayCluster, create a new RayCluster instance.
// One important part is that if this method deletes the old RayCluster, it will return instantly. It depends on the controller to call it again to generate the new RayCluster instance.
func (r *RayServiceReconciler) createRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1.RayService) (*rayv1.RayCluster, error) {
	logger := ctrl.LoggerFrom(ctx)
	rayClusterKey := common.RayServicePendingRayClusterNamespacedName(rayServiceInstance)

	logger.Info("createRayClusterInstance", "rayClusterInstanceName", rayClusterKey.Name)

	rayClusterInstance := &rayv1.RayCluster{}

	var err error
	// Loop until there is no pending RayCluster.
	err = r.Get(ctx, rayClusterKey, rayClusterInstance)

	// If RayCluster exists, it means the config is updated. Delete the previous RayCluster first.
	if err == nil {
		logger.Info("Ray cluster already exists, config changes. Need to recreate. Delete the pending one now.", "key", rayClusterKey.String(), "rayClusterInstance.Spec", rayClusterInstance.Spec, "rayServiceInstance.Spec.RayClusterSpec", rayServiceInstance.Spec.RayClusterSpec)
		delErr := r.Delete(ctx, rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground))
		if delErr == nil {
			// Go to next loop and check if the ray cluster is deleted.
			return nil, nil
		} else if !errors.IsNotFound(delErr) {
			return nil, delErr
		}
		// if error is `not found`, then continue.
	} else if !errors.IsNotFound(err) {
		return nil, err
		// if error is `not found`, then continue.
	}

	logger.Info("No pending RayCluster, creating RayCluster.")
	rayClusterInstance, err = r.constructRayClusterForRayService(ctx, rayServiceInstance, rayClusterKey.Name)
	if err != nil {
		return nil, err
	}
	if err = r.Create(ctx, rayClusterInstance); err != nil {
		return nil, err
	}
	logger.Info("created rayCluster for rayService", "rayCluster", rayClusterInstance)

	return rayClusterInstance, nil
}

func (r *RayServiceReconciler) constructRayClusterForRayService(ctx context.Context, rayService *rayv1.RayService, rayClusterName string) (*rayv1.RayCluster, error) {
	logger := ctrl.LoggerFrom(ctx)

	var err error
	rayClusterLabel := make(map[string]string)
	for k, v := range rayService.Labels {
		rayClusterLabel[k] = v
	}
	rayClusterLabel[utils.RayOriginatedFromCRNameLabelKey] = rayService.Name
	rayClusterLabel[utils.RayOriginatedFromCRDLabelKey] = utils.RayOriginatedFromCRDLabelValue(utils.RayServiceCRD)

	rayClusterAnnotations := make(map[string]string)
	for k, v := range rayService.Annotations {
		rayClusterAnnotations[k] = v
	}
	errContext := "Failed to serialize RayCluster config. " +
		"Manual config updates will NOT be tracked accurately. " +
		"Please tear down the cluster and apply a new config."
	rayClusterAnnotations[utils.HashWithoutReplicasAndWorkersToDeleteKey], err = generateHashWithoutReplicasAndWorkersToDelete(rayService.Spec.RayClusterSpec)
	if err != nil {
		logger.Error(err, errContext)
		return nil, err
	}
	rayClusterAnnotations[utils.NumWorkerGroupsKey] = strconv.Itoa(len(rayService.Spec.RayClusterSpec.WorkerGroupSpecs))

	rayCluster := &rayv1.RayCluster{
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

func (r *RayServiceReconciler) checkIfNeedSubmitServeDeployment(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster, serveStatus *rayv1.RayServiceStatus) bool {
	logger := ctrl.LoggerFrom(ctx)

	// If the Serve config has not been cached, update the Serve config.
	cacheKey := r.generateConfigKey(rayServiceInstance, rayClusterInstance.Name)
	cachedServeConfigV2, exist := r.ServeConfigs.Get(cacheKey)

	if !exist {
		logger.Info("shouldUpdate",
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

	// Handle the case that the head Pod has crashed and GCS FT is not enabled.
	if len(serveStatus.Applications) == 0 {
		logger.Info("shouldUpdate", "should create Serve applications", true,
			"reason",
			fmt.Sprintf(
				"No Serve application found in RayCluster %s, need to create serve applications. "+
					"A possible reason is the head Pod has crashed and GCS FT is not enabled. "+
					"Hence, the RayService CR's Serve application status is set to empty in the previous reconcile.",
				rayClusterInstance.Name))
		return true
	}

	// If the Serve config has been cached, check if it needs to be updated.
	shouldUpdate := false
	reason := fmt.Sprintf("Current Serve config matches cached Serve config, "+
		"and some deployments have been deployed for cluster %s", rayClusterInstance.Name)

	if cachedServeConfigV2 != rayServiceInstance.Spec.ServeConfigV2 {
		shouldUpdate = true
		reason = fmt.Sprintf("Current V2 Serve config doesn't match cached Serve config for cluster %s with key %s", rayClusterInstance.Name, cacheKey)
	}
	logger.Info("shouldUpdate", "shouldUpdateServe", shouldUpdate, "reason", reason, "cachedServeConfig", cachedServeConfigV2, "current Serve config", rayServiceInstance.Spec.ServeConfigV2)

	return shouldUpdate
}

func (r *RayServiceReconciler) updateServeDeployment(ctx context.Context, rayServiceInstance *rayv1.RayService, rayDashboardClient utils.RayDashboardClientInterface, clusterName string) error {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("updateServeDeployment", "V2 config", rayServiceInstance.Spec.ServeConfigV2)

	serveConfig := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(rayServiceInstance.Spec.ServeConfigV2), &serveConfig); err != nil {
		return err
	}

	configJson, err := json.Marshal(serveConfig)
	if err != nil {
		return fmt.Errorf("Failed to marshal converted serve config into bytes: %w", err)
	}
	logger.Info("updateServeDeployment", "MULTI_APP json config", string(configJson))
	if err := rayDashboardClient.UpdateDeployments(ctx, configJson); err != nil {
		err = fmt.Errorf(
			"Fail to create / update Serve applications. If you observe this error consistently, "+
				"please check \"Issue 5: Fail to create / update Serve applications.\" in "+
				"https://docs.ray.io/en/master/cluster/kubernetes/troubleshooting/rayservice-troubleshooting.html#kuberay-raysvc-troubleshoot for more details. "+
				"err: %v", err)
		return err
	}

	cacheKey := r.generateConfigKey(rayServiceInstance, clusterName)
	r.ServeConfigs.Set(cacheKey, rayServiceInstance.Spec.ServeConfigV2)
	logger.Info("updateServeDeployment", "message", fmt.Sprintf("Cached Serve config for Ray cluster %s with key %s", clusterName, cacheKey))
	return nil
}

// `getAndCheckServeStatus` gets Serve applications' and deployments' statuses and check whether the
// Serve applications are ready to serve incoming traffic or not. It returns two values:
//
// (1) `isReady` is used to determine whether the Serve applications in the RayCluster are ready to serve incoming traffic or not.
// (2) `err`: If `err` is not nil, it means that KubeRay failed to get Serve application statuses from the dashboard. We should take a look at dashboard rather than Ray Serve applications.

func (r *RayServiceReconciler) getAndCheckServeStatus(ctx context.Context, dashboardClient utils.RayDashboardClientInterface, rayServiceServeStatus *rayv1.RayServiceStatus) (bool, error) {
	logger := ctrl.LoggerFrom(ctx)
	var serveAppStatuses map[string]*utils.ServeApplicationStatus
	var err error
	if serveAppStatuses, err = dashboardClient.GetMultiApplicationStatus(ctx); err != nil {
		err = fmt.Errorf(
			"Failed to get Serve application statuses from the dashboard. "+
				"If you observe this error consistently, please check https://github.com/ray-project/kuberay/blob/master/docs/guidance/rayservice-troubleshooting.md for more details. "+
				"err: %v", err)
		return false, err
	}

	logger.Info("getAndCheckServeStatus", "prev statuses", rayServiceServeStatus.Applications, "serve statuses", serveAppStatuses)

	isReady := true
	timeNow := metav1.Now()

	newApplications := make(map[string]rayv1.AppStatus)
	for appName, app := range serveAppStatuses {
		if appName == "" {
			appName = utils.DefaultServeAppName
		}

		prevApplicationStatus := rayServiceServeStatus.Applications[appName]

		applicationStatus := rayv1.AppStatus{
			Message:              app.Message,
			Status:               app.Status,
			HealthLastUpdateTime: &timeNow,
			Deployments:          make(map[string]rayv1.ServeDeploymentStatus),
		}

		if isServeAppUnhealthyOrDeployedFailed(app.Status) {
			if isServeAppUnhealthyOrDeployedFailed(prevApplicationStatus.Status) {
				if prevApplicationStatus.HealthLastUpdateTime != nil {
					applicationStatus.HealthLastUpdateTime = prevApplicationStatus.HealthLastUpdateTime
					logger.Info("Ray Serve application is unhealthy", "appName", appName, "detail",
						fmt.Sprintf(
							"The status of the serve application %s has been UNHEALTHY or DEPLOY_FAILED since %v. ",
							appName, prevApplicationStatus.HealthLastUpdateTime))
				}
			}
		}

		// `isReady` is used to determine whether the Serve application is ready or not. The cluster switchover only happens when all Serve
		// applications in this RayCluster are ready so that the incoming traffic will not be dropped.
		if app.Status != rayv1.ApplicationStatusEnum.RUNNING {
			isReady = false
		}

		// Copy deployment statuses
		for deploymentName, deployment := range app.Deployments {
			deploymentStatus := rayv1.ServeDeploymentStatus{
				Status:               deployment.Status,
				Message:              deployment.Message,
				HealthLastUpdateTime: &timeNow,
			}

			if deployment.Status == rayv1.DeploymentStatusEnum.UNHEALTHY {
				prevStatus, exist := prevApplicationStatus.Deployments[deploymentName]
				if exist {
					if prevStatus.Status == rayv1.DeploymentStatusEnum.UNHEALTHY {
						deploymentStatus.HealthLastUpdateTime = prevStatus.HealthLastUpdateTime
					}
				}
			}
			applicationStatus.Deployments[deploymentName] = deploymentStatus
		}
		newApplications[appName] = applicationStatus
	}

	if len(newApplications) == 0 {
		logger.Info("No Serve application found. The RayCluster is not ready to serve requests. Set 'isReady' to false")
		isReady = false
	}
	rayServiceServeStatus.Applications = newApplications
	logger.Info("getAndCheckServeStatus", "new statuses", rayServiceServeStatus.Applications)
	return isReady, nil
}

func (r *RayServiceReconciler) generateConfigKey(rayServiceInstance *rayv1.RayService, clusterName string) string {
	return r.generateConfigKeyPrefix(rayServiceInstance) + clusterName
}

func (r *RayServiceReconciler) generateConfigKeyPrefix(rayServiceInstance *rayv1.RayService) string {
	return rayServiceInstance.Namespace + "/" + rayServiceInstance.Name + "/"
}

func (r *RayServiceReconciler) markRestartAndAddPendingClusterName(ctx context.Context, rayServiceInstance *rayv1.RayService) {
	logger := ctrl.LoggerFrom(ctx)

	// Generate RayCluster name for pending cluster.
	logger.Info("Current cluster is unhealthy, prepare to restart.", "Status", rayServiceInstance.Status)
	rayServiceInstance.Status.ServiceStatus = rayv1.Restarting
	rayServiceInstance.Status.PendingServiceStatus = rayv1.RayServiceStatus{
		RayClusterName: utils.GenerateRayClusterName(rayServiceInstance.Name),
	}
}

func (r *RayServiceReconciler) updateRayClusterInfo(ctx context.Context, rayServiceInstance *rayv1.RayService, healthyClusterName string) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("updateRayClusterInfo", "ActiveRayClusterName", rayServiceInstance.Status.ActiveServiceStatus.RayClusterName, "healthyClusterName", healthyClusterName)
	if rayServiceInstance.Status.ActiveServiceStatus.RayClusterName != healthyClusterName {
		rayServiceInstance.Status.ActiveServiceStatus = rayServiceInstance.Status.PendingServiceStatus
		rayServiceInstance.Status.PendingServiceStatus = rayv1.RayServiceStatus{}
	}
}

func (r *RayServiceReconciler) reconcileServices(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster, serviceType utils.ServiceType) error {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info(
		"reconcileServices", "serviceType", serviceType,
		"RayService name", rayServiceInstance.Name, "RayService namespace", rayServiceInstance.Namespace,
	)

	var newSvc *corev1.Service
	var err error

	switch serviceType {
	case utils.HeadService:
		newSvc, err = common.BuildHeadServiceForRayService(ctx, *rayServiceInstance, *rayClusterInstance)
	case utils.ServingService:
		newSvc, err = common.BuildServeServiceForRayService(ctx, *rayServiceInstance, *rayClusterInstance)
	default:
		return fmt.Errorf("unknown service type %v", serviceType)
	}

	if err != nil {
		return err
	}
	logger.Info("reconcileServices", "newSvc", newSvc)

	// Retrieve the Service from the Kubernetes cluster with the name and namespace.
	oldSvc := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Name: newSvc.Name, Namespace: rayServiceInstance.Namespace}, oldSvc)

	if err == nil {
		// Only update the service if the RayCluster switches.
		if newSvc.Spec.Selector[utils.RayClusterLabelKey] == oldSvc.Spec.Selector[utils.RayClusterLabelKey] {
			logger.Info(fmt.Sprintf("RayCluster %v's %v has already exists, skip Update", newSvc.Spec.Selector[utils.RayClusterLabelKey], serviceType))
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
		logger.Info(fmt.Sprintf("Update Kubernetes Service serviceType %v", serviceType))
		if updateErr := r.Update(ctx, oldSvc); updateErr != nil {
			return updateErr
		}
	} else if errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Create a Kubernetes Service for RayService serviceType %v", serviceType))
		if err := ctrl.SetControllerReference(rayServiceInstance, newSvc, r.Scheme); err != nil {
			return err
		}
		if createErr := r.Create(ctx, newSvc); createErr != nil {
			if errors.IsAlreadyExists(createErr) {
				logger.Info("The Kubernetes Service already exists, no need to create.")
				return nil
			}
			return createErr
		}
	} else {
		return err
	}

	return nil
}

func (r *RayServiceReconciler) updateStatusForActiveCluster(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	rayServiceInstance.Status.ActiveServiceStatus.RayClusterStatus = rayClusterInstance.Status

	var err error
	var clientURL string
	rayServiceStatus := &rayServiceInstance.Status.ActiveServiceStatus

	if clientURL, err = utils.FetchHeadServiceURL(ctx, r.Client, rayClusterInstance, utils.DashboardPortName); err != nil || clientURL == "" {
		return err
	}

	rayDashboardClient := r.dashboardClientFunc()
	if err := rayDashboardClient.InitClient(ctx, clientURL, rayClusterInstance); err != nil {
		return err
	}

	var isReady bool
	if isReady, err = r.getAndCheckServeStatus(ctx, rayDashboardClient, rayServiceStatus); err != nil {
		return err
	}

	logger.Info("Check serve health", "isReady", isReady)

	return err
}

// Reconciles the Serve applications on the RayCluster. Returns (ctrl.Result, isReady, error).
// The `isReady` flag indicates whether the RayCluster is ready to handle incoming traffic.
func (r *RayServiceReconciler) reconcileServe(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster, isActive bool) (ctrl.Result, bool, error) {
	logger := ctrl.LoggerFrom(ctx)
	rayServiceInstance.Status.ActiveServiceStatus.RayClusterStatus = rayClusterInstance.Status
	var err error
	var clientURL string
	var rayServiceStatus *rayv1.RayServiceStatus

	// Pick up service status to be updated.
	if isActive {
		rayServiceStatus = &rayServiceInstance.Status.ActiveServiceStatus
	} else {
		rayServiceStatus = &rayServiceInstance.Status.PendingServiceStatus
	}

	// Check if head pod is running and ready. If not, requeue the resource event to avoid
	// redundant custom resource status updates.
	//
	// TODO (kevin85421): Note that the Dashboard and GCS may take a few seconds to start up
	// after the head pod is running and ready. Hence, some requests to the Dashboard (e.g. `UpdateDeployments`) may fail.
	// This is not an issue since `UpdateDeployments` is an idempotent operation.
	logger.Info("Check the head Pod status of the pending RayCluster", "RayCluster name", rayClusterInstance.Name)
	if isRunningAndReady, err := r.isHeadPodRunningAndReady(ctx, rayClusterInstance); err != nil || !isRunningAndReady {
		if err != nil {
			logger.Error(err, "Failed to check if head Pod is running and ready!")
		} else {
			logger.Info("Skipping the update of Serve deployments because the Ray head Pod is not ready.")
		}
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, err
	}

	// TODO(architkulkarni): Check the RayVersion. If < 2.8.0, error.

	if clientURL, err = utils.FetchHeadServiceURL(ctx, r.Client, rayClusterInstance, utils.DashboardPortName); err != nil || clientURL == "" {
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, err
	}

	rayDashboardClient := r.dashboardClientFunc()
	if err := rayDashboardClient.InitClient(ctx, clientURL, rayClusterInstance); err != nil {
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, err
	}

	shouldUpdate := r.checkIfNeedSubmitServeDeployment(ctx, rayServiceInstance, rayClusterInstance, rayServiceStatus)
	if shouldUpdate {
		if err = r.updateServeDeployment(ctx, rayServiceInstance, rayDashboardClient, rayClusterInstance.Name); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1.WaitForServeDeploymentReady, err)
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, err
		}

		r.Recorder.Eventf(rayServiceInstance, "Normal", "SubmittedServeDeployment",
			"Controller sent API request to update Serve deployments on cluster %s", rayClusterInstance.Name)
	}

	var isReady bool
	if isReady, err = r.getAndCheckServeStatus(ctx, rayDashboardClient, rayServiceStatus); err != nil {
		err = r.updateState(ctx, rayServiceInstance, rayv1.FailedToGetServeDeploymentStatus, err)
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, err
	}

	logger.Info("Check serve health", "isReady", isReady, "isActive", isActive)

	if isReady {
		rayServiceInstance.Status.ServiceStatus = rayv1.Running
		r.updateRayClusterInfo(ctx, rayServiceInstance, rayClusterInstance.Name)
		r.Recorder.Event(rayServiceInstance, "Normal", "Running", "The Serve application is now running and healthy.")
	} else {
		rayServiceInstance.Status.ServiceStatus = rayv1.WaitForServeDeploymentReady
		if err := r.Status().Update(ctx, rayServiceInstance); err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, false, err
		}
		logger.Info("Mark cluster as waiting for Serve deployments", "rayCluster", rayClusterInstance)
	}

	return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, isReady, nil
}

func (r *RayServiceReconciler) labelHeadPodForServeStatus(ctx context.Context, rayClusterInstance *rayv1.RayCluster) error {
	headPod, err := r.getHeadPod(ctx, rayClusterInstance)
	if err != nil {
		return err
	}

	httpProxyClient := r.httpProxyClientFunc()
	httpProxyClient.InitClient()

	rayContainer := headPod.Spec.Containers[utils.RayContainerIndex]
	servingPort := utils.FindContainerPort(&rayContainer, utils.ServingPortName, utils.DefaultServingPort)
	httpProxyClient.SetHostIp(headPod.Status.PodIP, headPod.Namespace, headPod.Name, servingPort)

	if headPod.Labels == nil {
		headPod.Labels = make(map[string]string)
	}

	// Make a copy of the labels for comparison later, to decide whether we need to push an update.
	originalLabels := make(map[string]string, len(headPod.Labels))
	for key, value := range headPod.Labels {
		originalLabels[key] = value
	}

	if err = httpProxyClient.CheckProxyActorHealth(ctx); err == nil {
		headPod.Labels[utils.RayClusterServingServiceLabelKey] = utils.EnableRayClusterServingServiceTrue
	} else {
		headPod.Labels[utils.RayClusterServingServiceLabelKey] = utils.EnableRayClusterServingServiceFalse
	}

	if !reflect.DeepEqual(originalLabels, headPod.Labels) {
		if updateErr := r.Update(ctx, headPod); updateErr != nil {
			return updateErr
		}
	}

	return nil
}

func getClusterAction(oldSpec rayv1.RayClusterSpec, newSpec rayv1.RayClusterSpec) (ClusterAction, error) {
	// Return the appropriate action based on the difference in the old and new RayCluster specs.

	// Case 1: If everything is identical except for the Replicas and WorkersToDelete of
	// each WorkerGroup, then do nothing.
	sameHash, err := compareRayClusterJsonHash(oldSpec, newSpec, generateHashWithoutReplicasAndWorkersToDelete)
	if err != nil {
		return DoNothing, err
	}
	if sameHash {
		return DoNothing, nil
	}

	// Case 2: Otherwise, if everything is identical except for the Replicas and WorkersToDelete of
	// the existing workergroups, and one or more new workergroups are added at the end, then update the cluster.
	newSpecWithoutWorkerGroups := newSpec.DeepCopy()
	if len(newSpec.WorkerGroupSpecs) > len(oldSpec.WorkerGroupSpecs) {
		// Remove the new worker groups from the new spec.
		newSpecWithoutWorkerGroups.WorkerGroupSpecs = newSpecWithoutWorkerGroups.WorkerGroupSpecs[:len(oldSpec.WorkerGroupSpecs)]

		sameHash, err = compareRayClusterJsonHash(oldSpec, *newSpecWithoutWorkerGroups, generateHashWithoutReplicasAndWorkersToDelete)
		if err != nil {
			return DoNothing, err
		}
		if sameHash {
			return Update, nil
		}
	}

	// Case 3: Otherwise, rollout a new cluster.
	return RolloutNew, nil
}

func generateHashWithoutReplicasAndWorkersToDelete(rayClusterSpec rayv1.RayClusterSpec) (string, error) {
	// Mute certain fields that will not trigger new RayCluster preparation. For example,
	// Autoscaler will update `Replicas` and `WorkersToDelete` when scaling up/down.
	updatedRayClusterSpec := rayClusterSpec.DeepCopy()
	for i := 0; i < len(updatedRayClusterSpec.WorkerGroupSpecs); i++ {
		updatedRayClusterSpec.WorkerGroupSpecs[i].Replicas = nil
		updatedRayClusterSpec.WorkerGroupSpecs[i].MaxReplicas = nil
		updatedRayClusterSpec.WorkerGroupSpecs[i].MinReplicas = nil
		updatedRayClusterSpec.WorkerGroupSpecs[i].ScaleStrategy.WorkersToDelete = nil
	}

	// Generate a hash for the RayClusterSpec.
	return utils.GenerateJsonHash(updatedRayClusterSpec)
}

func compareRayClusterJsonHash(spec1 rayv1.RayClusterSpec, spec2 rayv1.RayClusterSpec, hashFunc func(rayv1.RayClusterSpec) (string, error)) (bool, error) {
	hash1, err1 := hashFunc(spec1)
	if err1 != nil {
		return false, err1
	}

	hash2, err2 := hashFunc(spec2)
	if err2 != nil {
		return false, err2
	}
	return hash1 == hash2, nil
}

// isHeadPodRunningAndReady checks if the head pod of the RayCluster is running and ready.
func (r *RayServiceReconciler) isHeadPodRunningAndReady(ctx context.Context, instance *rayv1.RayCluster) (bool, error) {
	headPod, err := r.getHeadPod(ctx, instance)
	if err != nil {
		return false, err
	}
	return utils.IsRunningAndReady(headPod), nil
}

func isServeAppUnhealthyOrDeployedFailed(appStatus string) bool {
	return appStatus == rayv1.ApplicationStatusEnum.UNHEALTHY || appStatus == rayv1.ApplicationStatusEnum.DEPLOY_FAILED
}

// TODO: Move this function to util.go and always use this function to retrieve the head Pod.
func (r *RayServiceReconciler) getHeadPod(ctx context.Context, instance *rayv1.RayCluster) (*corev1.Pod, error) {
	podList := corev1.PodList{}
	if err := r.List(ctx, &podList, common.RayClusterHeadPodsAssociationOptions(instance).ToListOptions()...); err != nil {
		return nil, err
	}

	if len(podList.Items) != 1 {
		return nil, fmt.Errorf("Found %d head pods for RayCluster %s in the namespace %s", len(podList.Items), instance.Name, instance.Namespace)
	}

	return &podList.Items[0], nil
}
