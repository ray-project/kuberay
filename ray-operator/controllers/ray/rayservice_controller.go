package ray

import (
	"context"
	errstd "errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/lru"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"

	cmap "github.com/orcaman/concurrent-map/v2"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
	// Currently, the Ray dashboard doesn't cache the Serve application config.
	// To avoid reapplying the same config repeatedly, cache the config in this map.
	// Cache key is the combination of RayService namespace and name.
	// Cache value is map of RayCluster name to Serve application config.
	ServeConfigs                 *lru.Cache
	RayClusterDeletionTimestamps cmap.ConcurrentMap[string, time.Time]
	dashboardClientFunc          func() utils.RayDashboardClientInterface
	httpProxyClientFunc          func() utils.RayHttpProxyClientInterface
}

// NewRayServiceReconciler returns a new reconcile.Reconciler
func NewRayServiceReconciler(_ context.Context, mgr manager.Manager, provider utils.ClientProvider) *RayServiceReconciler {
	dashboardClientFunc := provider.GetDashboardClient(mgr)
	httpProxyClientFunc := provider.GetHttpProxyClient(mgr)
	return &RayServiceReconciler{
		Client:                       mgr.GetClient(),
		Scheme:                       mgr.GetScheme(),
		Recorder:                     mgr.GetEventRecorderFor("rayservice-controller"),
		ServeConfigs:                 lru.New(utils.ServeConfigLRUSize),
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

	rayServiceInstance, err := r.getRayServiceInstance(ctx, request)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	originalRayServiceInstance := rayServiceInstance.DeepCopy()

	if err := utils.ValidateRayServiceMetadata(rayServiceInstance.ObjectMeta); err != nil {
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.InvalidRayServiceMetadata),
			"The RayService metadata is invalid %s/%s: %v", rayServiceInstance.Namespace, rayServiceInstance.Name, err)
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
	}
	if err := utils.ValidateRayServiceSpec(rayServiceInstance); err != nil {
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.InvalidRayServiceSpec),
			"The RayService spec is invalid %s/%s: %v", rayServiceInstance.Namespace, rayServiceInstance.Name, err)
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
	}

	r.cleanUpServeConfigCache(ctx, rayServiceInstance)
	if err = r.cleanUpRayClusterInstance(ctx, rayServiceInstance); err != nil {
		return ctrl.Result{}, err
	}

	// Find active and pending ray cluster objects given current service name.
	var activeRayClusterInstance, pendingRayClusterInstance *rayv1.RayCluster
	if activeRayClusterInstance, pendingRayClusterInstance, err = r.reconcileRayCluster(ctx, rayServiceInstance); err != nil {
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, client.IgnoreNotFound(err)
	}

	// Check both active and pending Ray clusters to see if the head Pod is ready to serve requests.
	// This is important to ensure the reliability of the serve service because the head Pod cannot
	// rely on readiness probes to determine serve readiness.
	if err := r.updateHeadPodServeLabel(ctx, rayServiceInstance, activeRayClusterInstance, rayServiceInstance.Spec.ExcludeHeadPodFromServeSvc); err != nil {
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
	}
	if err := r.updateHeadPodServeLabel(ctx, rayServiceInstance, pendingRayClusterInstance, rayServiceInstance.Spec.ExcludeHeadPodFromServeSvc); err != nil {
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
	}

	// Reconcile serve applications for active and/or pending clusters
	// 1. If there is a pending cluster, reconcile serve applications for the pending cluster.
	// 2. If there are both active and pending clusters, reconcile serve applications for the pending cluster only.
	// 3. If there is no pending cluster, reconcile serve applications for the active cluster.
	var isActiveClusterReady, isPendingClusterReady bool = false, false
	var activeClusterServeApplications, pendingClusterServeApplications map[string]rayv1.AppStatus = nil, nil
	if pendingRayClusterInstance != nil {
		logger.Info("Reconciling the Serve applications for pending cluster", "clusterName", pendingRayClusterInstance.Name)
		if isPendingClusterReady, pendingClusterServeApplications, err = r.reconcileServe(ctx, rayServiceInstance, pendingRayClusterInstance); err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
	}
	if activeRayClusterInstance != nil && pendingRayClusterInstance == nil {
		// Only reconcile serve applications for the active cluster when there is no pending cluster. That is, during the upgrade process,
		// in-place update and updating the serve application status for the active cluster will not work.
		logger.Info("Reconciling the Serve applications for active cluster", "clusterName", activeRayClusterInstance.Name)
		if isActiveClusterReady, activeClusterServeApplications, err = r.reconcileServe(ctx, rayServiceInstance, activeRayClusterInstance); err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
	}

	// Reconcile K8s services and make sure it points to the correct RayCluster.
	var headSvc, serveSvc *corev1.Service
	if isPendingClusterReady || isActiveClusterReady {
		targetCluster := activeRayClusterInstance
		logMsg := "Reconciling K8s services to point to the active Ray cluster."

		if isPendingClusterReady {
			targetCluster = pendingRayClusterInstance
			logMsg = "Reconciling K8s services to point to the pending Ray cluster to switch traffic because it is ready."
		}

		logger.Info(logMsg)
		headSvc, serveSvc, err = r.reconcileServicesToReadyCluster(ctx, rayServiceInstance, targetCluster)
		if err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}

		if headSvc == nil || serveSvc == nil {
			panic("Both head and serve services are nil before calculate RayService status. " +
				"This should never happen. Please open a GitHub issue in the KubeRay repository.")
		}
	}

	// Calculate the status of the RayService based on K8s resources.
	if err := r.calculateStatus(
		ctx,
		rayServiceInstance,
		headSvc,
		serveSvc,
		activeRayClusterInstance,
		pendingRayClusterInstance,
		activeClusterServeApplications,
		pendingClusterServeApplications,
	); err != nil {
		return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
	}

	// Final status update for any CR modification.
	if inconsistentRayServiceStatuses(ctx, originalRayServiceInstance.Status, rayServiceInstance.Status) {
		rayServiceInstance.Status.LastUpdateTime = &metav1.Time{Time: time.Now()}
		if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, errStatus
		}
	}
	return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, nil
}

func (r *RayServiceReconciler) reconcileServicesToReadyCluster(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster) (*corev1.Service, *corev1.Service, error) {
	// Create K8s services if they don't exist. If they do exist, update the services to point to the RayCluster passed in.
	headSvc, err := r.reconcileServices(ctx, rayServiceInstance, rayClusterInstance, utils.HeadService)
	if err != nil {
		return headSvc, nil, err
	}
	serveSvc, err := r.reconcileServices(ctx, rayServiceInstance, rayClusterInstance, utils.ServingService)
	if err != nil {
		return headSvc, serveSvc, err
	}
	return headSvc, serveSvc, nil
}

func (r *RayServiceReconciler) calculateStatus(ctx context.Context, rayServiceInstance *rayv1.RayService, headSvc, serveSvc *corev1.Service, activeCluster, pendingCluster *rayv1.RayCluster, activeClusterServeApplications, pendingClusterServeApplications map[string]rayv1.AppStatus) error {
	logger := ctrl.LoggerFrom(ctx)

	rayServiceInstance.Status.ObservedGeneration = rayServiceInstance.ObjectMeta.Generation

	// Update RayClusterStatus in RayService status.
	var activeClusterStatus, pendingClusterStatus rayv1.RayClusterStatus
	if activeCluster != nil {
		activeClusterStatus = activeCluster.Status
	}
	if pendingCluster != nil {
		pendingClusterStatus = pendingCluster.Status
	}
	rayServiceInstance.Status.ActiveServiceStatus.RayClusterStatus = activeClusterStatus
	rayServiceInstance.Status.PendingServiceStatus.RayClusterStatus = pendingClusterStatus

	// Update serve application status in RayService status.
	rayServiceInstance.Status.ActiveServiceStatus.Applications = activeClusterServeApplications
	rayServiceInstance.Status.PendingServiceStatus.Applications = pendingClusterServeApplications

	isPendingClusterServing := false
	if headSvc != nil && serveSvc != nil {
		pendingClusterName := rayServiceInstance.Status.PendingServiceStatus.RayClusterName
		activeClusterName := rayServiceInstance.Status.ActiveServiceStatus.RayClusterName

		// Promote the pending cluster to the active cluster if both RayService's head and serve services
		// have already pointed to the pending cluster.
		clusterName := utils.GetRayClusterNameFromService(headSvc)
		if clusterName != utils.GetRayClusterNameFromService(serveSvc) {
			panic("headSvc and serveSvc are not pointing to the same cluster")
		}
		// Verify cluster name matches either pending or active cluster
		if clusterName != pendingClusterName && clusterName != activeClusterName {
			panic("clusterName is not equal to pendingCluster or activeCluster")
		}
		isPendingClusterServing = clusterName == pendingClusterName

		// If services point to a different cluster than the active one, promote pending to active
		logger.Info("calculateStatus", "clusterSvcPointingTo", clusterName, "pendingClusterName", pendingClusterName, "activeClusterName", activeClusterName)
		if activeClusterName != clusterName {
			logger.Info("Promoting pending cluster to active",
				"oldCluster", rayServiceInstance.Status.ActiveServiceStatus.RayClusterName,
				"newCluster", clusterName)
			rayServiceInstance.Status.ActiveServiceStatus = rayServiceInstance.Status.PendingServiceStatus
			rayServiceInstance.Status.PendingServiceStatus = rayv1.RayServiceStatus{}
		}
	}

	if shouldPrepareNewCluster(ctx, rayServiceInstance, activeCluster, pendingCluster, isPendingClusterServing) {
		rayServiceInstance.Status.PendingServiceStatus = rayv1.RayServiceStatus{
			RayClusterName: utils.GenerateRayClusterName(rayServiceInstance.Name),
		}
		logger.Info("Preparing a new pending RayCluster instance by setting RayClusterName",
			"clusterName", rayServiceInstance.Status.PendingServiceStatus.RayClusterName)
	}

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
	if numServeEndpoints > math.MaxInt32 {
		return errstd.New("numServeEndpoints exceeds math.MaxInt32")
	}
	rayServiceInstance.Status.NumServeEndpoints = int32(numServeEndpoints) //nolint:gosec // This is a false positive from gosec. See https://github.com/securego/gosec/issues/1212 for more details.
	calculateConditions(rayServiceInstance)

	// The definition of `ServiceStatus` is equivalent to the `RayServiceReady` condition
	rayServiceInstance.Status.ServiceStatus = rayv1.NotRunning
	if meta.IsStatusConditionTrue(rayServiceInstance.Status.Conditions, string(rayv1.RayServiceReady)) {
		rayServiceInstance.Status.ServiceStatus = rayv1.Running
	}
	return nil
}

func calculateConditions(rayServiceInstance *rayv1.RayService) {
	if rayServiceInstance.Status.Conditions == nil {
		rayServiceInstance.Status.Conditions = []metav1.Condition{}
	}
	if len(rayServiceInstance.Status.Conditions) == 0 {
		message := "RayService is initializing"
		setCondition(rayServiceInstance, rayv1.RayServiceReady, metav1.ConditionFalse, rayv1.RayServiceInitializing, message)
		setCondition(rayServiceInstance, rayv1.UpgradeInProgress, metav1.ConditionFalse, rayv1.RayServiceInitializing, message)
	}
	if rayServiceInstance.Status.NumServeEndpoints > 0 {
		setCondition(rayServiceInstance, rayv1.RayServiceReady, metav1.ConditionTrue, rayv1.NonZeroServeEndpoints, "Number of serve endpoints is greater than 0")
	} else if meta.IsStatusConditionTrue(rayServiceInstance.Status.Conditions, string(rayv1.RayServiceReady)) {
		setCondition(rayServiceInstance, rayv1.RayServiceReady, metav1.ConditionFalse, rayv1.ZeroServeEndpoints, "Number of serve endpoints dropped to 0")
	}

	activeClusterName := rayServiceInstance.Status.ActiveServiceStatus.RayClusterName
	pendingClusterName := rayServiceInstance.Status.PendingServiceStatus.RayClusterName
	if activeClusterName != "" && pendingClusterName != "" {
		setCondition(rayServiceInstance, rayv1.UpgradeInProgress, metav1.ConditionTrue, rayv1.BothActivePendingClustersExist, "Both active and pending Ray clusters exist")
	} else if activeClusterName != "" {
		setCondition(rayServiceInstance, rayv1.UpgradeInProgress, metav1.ConditionFalse, rayv1.NoPendingCluster, "Active Ray cluster exists and no pending Ray cluster")
	} else {
		cond := meta.FindStatusCondition(rayServiceInstance.Status.Conditions, string(rayv1.UpgradeInProgress))
		if cond == nil || cond.Reason != string(rayv1.RayServiceInitializing) {
			setCondition(rayServiceInstance, rayv1.UpgradeInProgress, metav1.ConditionUnknown, rayv1.NoActiveCluster, "No active Ray cluster exists, and the RayService is not initializing. Please open a GitHub issue in the KubeRay repository.")
		}
	}
}

func setCondition(rayServiceInstance *rayv1.RayService, conditionType rayv1.RayServiceConditionType, status metav1.ConditionStatus, reason rayv1.RayServiceConditionReason, message string) {
	condition := metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: rayServiceInstance.Status.ObservedGeneration,
	}
	meta.SetStatusCondition(&rayServiceInstance.Status.Conditions, condition)
}

// Checks whether the old and new RayServiceStatus are inconsistent by comparing different fields.
// The RayClusterStatus field is only for observability in RayService CR, and changes to it will not trigger the status update.
func inconsistentRayServiceStatus(ctx context.Context, oldStatus rayv1.RayServiceStatus, newStatus rayv1.RayServiceStatus) bool {
	logger := ctrl.LoggerFrom(ctx)
	if oldStatus.RayClusterName != newStatus.RayClusterName {
		logger.Info("inconsistentRayServiceStatus RayService RayClusterName", "oldRayClusterName", oldStatus.RayClusterName, "newRayClusterName", newStatus.RayClusterName)
		return true
	}

	if len(oldStatus.Applications) != len(newStatus.Applications) {
		return true
	}

	var ok bool
	for appName, newAppStatus := range newStatus.Applications {
		var oldAppStatus rayv1.AppStatus
		if oldAppStatus, ok = oldStatus.Applications[appName]; !ok {
			logger.Info("inconsistentRayServiceStatus RayService new application found", "appName", appName)
			return true
		}

		if oldAppStatus.Status != newAppStatus.Status {
			logger.Info("inconsistentRayServiceStatus RayService application status changed", "appName", appName, "oldStatus", oldAppStatus.Status, "newStatus", newAppStatus.Status)
			return true
		} else if oldAppStatus.Message != newAppStatus.Message {
			logger.Info("inconsistentRayServiceStatus RayService application status message changed", "appName", appName, "oldStatus", oldAppStatus.Message, "newStatus", newAppStatus.Message)
			return true
		}

		if len(oldAppStatus.Deployments) != len(newAppStatus.Deployments) {
			return true
		}

		for deploymentName, newDeploymentStatus := range newAppStatus.Deployments {
			var oldDeploymentStatus rayv1.ServeDeploymentStatus
			if oldDeploymentStatus, ok = oldAppStatus.Deployments[deploymentName]; !ok {
				logger.Info("inconsistentRayServiceStatus RayService new deployment found in application", "deploymentName", deploymentName, "appName", appName)
				return true
			}

			if oldDeploymentStatus.Status != newDeploymentStatus.Status {
				logger.Info("inconsistentRayServiceStatus RayService DeploymentStatus changed", "oldDeploymentStatus", oldDeploymentStatus.Status, "newDeploymentStatus", newDeploymentStatus.Status)
				return true
			} else if oldDeploymentStatus.Message != newDeploymentStatus.Message {
				logger.Info("inconsistentRayServiceStatus RayService deployment status message changed", "oldDeploymentStatus", oldDeploymentStatus.Message, "newDeploymentStatus", newDeploymentStatus.Message)
				return true
			}
		}
	}

	return false
}

// Determine whether to update the status of the RayService instance.
func inconsistentRayServiceStatuses(ctx context.Context, oldStatus rayv1.RayServiceStatuses, newStatus rayv1.RayServiceStatuses) bool {
	logger := ctrl.LoggerFrom(ctx)
	if oldStatus.ServiceStatus != newStatus.ServiceStatus {
		logger.Info("inconsistentRayServiceStatus RayService ServiceStatus changed", "oldServiceStatus", oldStatus.ServiceStatus, "newServiceStatus", newStatus.ServiceStatus)
		return true
	}

	if oldStatus.NumServeEndpoints != newStatus.NumServeEndpoints {
		logger.Info("inconsistentRayServiceStatus RayService NumServeEndpoints changed", "oldNumServeEndpoints", oldStatus.NumServeEndpoints, "newNumServeEndpoints", newStatus.NumServeEndpoints)
		return true
	}

	if !reflect.DeepEqual(oldStatus.Conditions, newStatus.Conditions) {
		logger.Info("inconsistentRayServiceStatus RayService Conditions changed")
		return true
	}

	if inconsistentRayServiceStatus(ctx, oldStatus.ActiveServiceStatus, newStatus.ActiveServiceStatus) {
		logger.Info("inconsistentRayServiceStatus RayService ActiveServiceStatus changed")
		return true
	}

	if inconsistentRayServiceStatus(ctx, oldStatus.PendingServiceStatus, newStatus.PendingServiceStatus) {
		logger.Info("inconsistentRayServiceStatus RayService PendingServiceStatus changed")
		return true
	}

	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayServiceReconciler) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayService{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		Owns(&rayv1.RayCluster{}).
		Owns(&corev1.Service{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: reconcileConcurrency,
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

func isZeroDowntimeUpgradeEnabled(ctx context.Context, upgradeStrategy *rayv1.RayServiceUpgradeStrategy) bool {
	// For LLM serving, some users might not have sufficient GPU resources to run two RayClusters simultaneously.
	// Therefore, KubeRay offers ENABLE_ZERO_DOWNTIME as a feature flag for zero-downtime upgrades.
	// There are two ways to enable zero downtime upgrade. Through ENABLE_ZERO_DOWNTIME env var or setting Spec.UpgradeStrategy.Type.
	// If no fields are set, zero downtime upgrade by default is enabled.
	// Spec.UpgradeStrategy.Type takes precedence over ENABLE_ZERO_DOWNTIME.
	logger := ctrl.LoggerFrom(ctx)
	if upgradeStrategy != nil {
		upgradeType := upgradeStrategy.Type
		if upgradeType != nil {
			if *upgradeType != rayv1.NewCluster {
				logger.Info("Zero-downtime upgrade is disabled because UpgradeStrategy.Type is not set to NewCluster.")
				return false
			}
			return true
		}
	}
	zeroDowntimeEnvVar := os.Getenv(ENABLE_ZERO_DOWNTIME)
	if strings.ToLower(zeroDowntimeEnvVar) == "false" {
		logger.Info("Zero-downtime upgrade is disabled because ENABLE_ZERO_DOWNTIME is set to false.")
		return false
	}
	return true
}

// `reconcileRayCluster` reconciles the active and pending Ray clusters. There are 4 possible cases:
// (1) Create a new pending cluster. (2) Update the active cluster. (3) Update the pending cluster. (4) Do nothing.
func (r *RayServiceReconciler) reconcileRayCluster(ctx context.Context, rayServiceInstance *rayv1.RayService) (*rayv1.RayCluster, *rayv1.RayCluster, error) {
	logger := ctrl.LoggerFrom(ctx)
	var err error

	// Get active cluster and pending cluster instances.
	activeRayCluster, err := r.getRayClusterByNamespacedName(ctx, common.RayServiceActiveRayClusterNamespacedName(rayServiceInstance))
	if err != nil {
		return nil, nil, err
	}

	pendingRayCluster, err := r.getRayClusterByNamespacedName(ctx, common.RayServicePendingRayClusterNamespacedName(rayServiceInstance))
	if err != nil {
		return nil, nil, err
	}

	if rayServiceInstance.Status.PendingServiceStatus.RayClusterName != "" && pendingRayCluster == nil {
		logger.Info("Creating a new pending RayCluster instance")
		pendingRayCluster, err = r.createRayClusterInstance(ctx, rayServiceInstance)
		return activeRayCluster, pendingRayCluster, err
	}

	if shouldUpdateCluster(rayServiceInstance, activeRayCluster, true) {
		// TODO(kevin85421): We should not reconstruct the cluster to update it. This will cause issues if autoscaler is enabled.
		logger.Info("Updating the active RayCluster instance", "clusterName", activeRayCluster.Name)
		goalCluster, err := constructRayClusterForRayService(rayServiceInstance, activeRayCluster.Name, r.Scheme)
		if err != nil {
			return nil, nil, err
		}
		modifyRayCluster(ctx, activeRayCluster, goalCluster)
		if err = r.Update(ctx, activeRayCluster); err != nil {
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToUpdateRayCluster), "Failed to update the active RayCluster %s/%s: %v", activeRayCluster.Namespace, activeRayCluster.Name, err)
		}
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedRayCluster), "Updated the active RayCluster %s/%s", activeRayCluster.Namespace, activeRayCluster.Name)
		return activeRayCluster, pendingRayCluster, err
	}

	if shouldUpdateCluster(rayServiceInstance, pendingRayCluster, false) {
		// TODO(kevin85421): We should not reconstruct the cluster to update it. This will cause issues if autoscaler is enabled.
		logger.Info("Updating the pending RayCluster instance", "clusterName", pendingRayCluster.Name)
		goalCluster, err := constructRayClusterForRayService(rayServiceInstance, pendingRayCluster.Name, r.Scheme)
		if err != nil {
			return nil, nil, err
		}
		modifyRayCluster(ctx, pendingRayCluster, goalCluster)
		if err = r.Update(ctx, pendingRayCluster); err != nil {
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToUpdateRayCluster), "Failed to update the pending RayCluster %s/%s: %v", pendingRayCluster.Namespace, pendingRayCluster.Name, err)
		}
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedRayCluster), "Updated the pending RayCluster %s/%s", pendingRayCluster.Namespace, pendingRayCluster.Name)
		return activeRayCluster, pendingRayCluster, nil
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
	for _, rayClusterInstance := range rayClusterList.Items {
		if rayClusterInstance.Name != rayServiceInstance.Status.ActiveServiceStatus.RayClusterName && rayClusterInstance.Name != rayServiceInstance.Status.PendingServiceStatus.RayClusterName {
			cachedTimestamp, exists := r.RayClusterDeletionTimestamps.Get(rayClusterInstance.Name)
			if !exists {
				deletionTimestamp := metav1.Now().Add(RayClusterDeletionDelayDuration)
				r.RayClusterDeletionTimestamps.Set(rayClusterInstance.Name, deletionTimestamp)
				logger.Info(
					"Scheduled dangling RayCluster for deletion",
					"rayClusterName", rayClusterInstance.Name,
					"deletionTimestamp", deletionTimestamp,
				)
			} else {
				reasonForDeletion := ""
				if time.Now().After(cachedTimestamp) {
					reasonForDeletion = fmt.Sprintf("Deletion timestamp %s "+
						"for RayCluster %s has passed. Deleting cluster "+
						"immediately.", cachedTimestamp, rayClusterInstance.Name)
				}

				if reasonForDeletion != "" {
					logger.Info("reconcileRayCluster", "delete Ray cluster", rayClusterInstance.Name, "reason", reasonForDeletion)
					if err := r.Delete(ctx, &rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
						r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToDeleteRayCluster), "Failed to delete the RayCluster %s/%s: %v", rayClusterInstance.Namespace, rayClusterInstance.Name, err)
						return err
					}
					r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.DeletedRayCluster), "Deleted the RayCluster %s/%s", rayClusterInstance.Namespace, rayClusterInstance.Name)
				}
			}
		}
	}

	return nil
}

func (r *RayServiceReconciler) getRayClusterByNamespacedName(ctx context.Context, clusterKey client.ObjectKey) (*rayv1.RayCluster, error) {
	if clusterKey.Name == "" {
		return nil, nil
	}

	rayCluster := &rayv1.RayCluster{}
	if err := r.Get(ctx, clusterKey, rayCluster); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return rayCluster, nil
}

// cleanUpServeConfigCache cleans up the unused serve applications config in the cached map.
func (r *RayServiceReconciler) cleanUpServeConfigCache(ctx context.Context, rayServiceInstance *rayv1.RayService) {
	logger := ctrl.LoggerFrom(ctx)
	activeRayClusterName := rayServiceInstance.Status.ActiveServiceStatus.RayClusterName
	pendingRayClusterName := rayServiceInstance.Status.PendingServiceStatus.RayClusterName

	cacheKey := rayServiceInstance.Namespace + "/" + rayServiceInstance.Name
	cacheValue, exist := r.ServeConfigs.Get(cacheKey)
	if !exist {
		return
	}
	clusterNameToServeConfig := cacheValue.(cmap.ConcurrentMap[string, string])

	for key := range clusterNameToServeConfig.Items() {
		if key == activeRayClusterName || key == pendingRayClusterName {
			continue
		}
		logger.Info("Remove stale serve application config", "remove key", key, "activeRayClusterName", activeRayClusterName, "pendingRayClusterName", pendingRayClusterName)
		clusterNameToServeConfig.Remove(key)
	}
}

func shouldUpdateCluster(rayServiceInstance *rayv1.RayService, cluster *rayv1.RayCluster, isActiveCluster bool) bool {
	// Check whether to update the RayCluster or not.
	if cluster == nil {
		return false
	}
	if isActiveCluster {
		if meta.IsStatusConditionTrue(rayServiceInstance.Status.Conditions, string(rayv1.UpgradeInProgress)) {
			// If the RayService is upgrading, the `RayService.Spec.RayClusterSpec` should only be compared with the
			// pending cluster. The active cluster should not be updated.
			return false
		}

		// If the KubeRay version has changed, update the RayCluster to get the cluster hash and new KubeRay version.
		version := cluster.ObjectMeta.Annotations[utils.KubeRayVersion]
		if version != utils.KUBERAY_VERSION {
			return true
		}
	}

	if isClusterSpecHashEqual(rayServiceInstance, cluster, false) {
		// The RayCluster spec matches the cluster spec in the RayService. No need to update the cluster.
		return false
	}
	// Update the RayCluster when the cluster spec in the RayService differs from the RayCluster, but only if the difference
	// is in the number of worker groups and the new worker groups are added at the end.
	return isClusterSpecHashEqual(rayServiceInstance, cluster, true)
}

func isClusterSpecHashEqual(rayServiceInstance *rayv1.RayService, cluster *rayv1.RayCluster, partial bool) bool {
	// If `partial` is true, only compare the first `len(cluster.Spec.WorkerGroupSpecs)` worker groups in the CR spec.
	clusterHash := cluster.ObjectMeta.Annotations[utils.HashWithoutReplicasAndWorkersToDeleteKey]
	goalClusterHash := ""
	if !partial {
		goalClusterHash, _ = generateHashWithoutReplicasAndWorkersToDelete(rayServiceInstance.Spec.RayClusterSpec)
	} else {
		// If everything is identical except for the Replicas and WorkersToDelete of
		// the existing workergroups, and one or more new workergroups are added at the end, then update the cluster.
		clusterNumWorkerGroups, err := strconv.Atoi(cluster.ObjectMeta.Annotations[utils.NumWorkerGroupsKey])
		if err != nil {
			return true
		}
		goalNumWorkerGroups := len(rayServiceInstance.Spec.RayClusterSpec.WorkerGroupSpecs)
		if goalNumWorkerGroups >= clusterNumWorkerGroups {

			// Remove the new workergroup(s) from the end before calculating the hash.
			goalClusterSpec := rayServiceInstance.Spec.RayClusterSpec.DeepCopy()
			goalClusterSpec.WorkerGroupSpecs = goalClusterSpec.WorkerGroupSpecs[:clusterNumWorkerGroups]

			// Generate the hash of the old worker group specs.
			goalClusterHash, err = generateHashWithoutReplicasAndWorkersToDelete(*goalClusterSpec)
			if err != nil {
				return true
			}
		}
	}
	return clusterHash == goalClusterHash
}

func shouldPrepareNewCluster(ctx context.Context, rayServiceInstance *rayv1.RayService, activeRayCluster, pendingRayCluster *rayv1.RayCluster, isPendingClusterServing bool) bool {
	if isPendingClusterServing {
		return false
	}
	if activeRayCluster == nil && pendingRayCluster == nil {
		// Both active and pending clusters are nil, which means the RayService has just been created.
		// Create a new pending cluster.
		return true
	}
	cluster := pendingRayCluster
	if cluster == nil {
		cluster = activeRayCluster
	}
	if isClusterSpecHashEqual(rayServiceInstance, cluster, false) {
		// The RayCluster spec matches the cluster spec in the RayService. No need to create a new pending cluster.
		return false
	}
	if isClusterSpecHashEqual(rayServiceInstance, cluster, true) {
		// KubeRay should update the RayCluster instead of creating a new one.
		return false
	}
	return isZeroDowntimeUpgradeEnabled(ctx, rayServiceInstance.Spec.UpgradeStrategy)
}

// `modifyRayCluster` updates `currentCluster` in place based on `goalCluster`. `currentCluster` is the
// current RayCluster retrieved from the informer cache, and `goalCluster` is the target state of the
// RayCluster derived from the RayService spec.
func modifyRayCluster(ctx context.Context, currentCluster, goalCluster *rayv1.RayCluster) {
	logger := ctrl.LoggerFrom(ctx)

	if currentCluster.Name != goalCluster.Name || currentCluster.Namespace != goalCluster.Namespace {
		panic(fmt.Sprintf(
			"currentCluster and goalCluster have different names or namespaces: "+
				"%s/%s != %s/%s",
			currentCluster.Namespace,
			currentCluster.Name,
			goalCluster.Namespace,
			goalCluster.Name,
		))
	}
	logger.Info("updateRayClusterInstance", "Name", goalCluster.Name, "Namespace", goalCluster.Namespace)

	// Update the fetched RayCluster with new changes
	currentCluster.Spec = goalCluster.Spec

	// Update the labels and annotations
	currentCluster.Labels = goalCluster.Labels
	currentCluster.Annotations = goalCluster.Annotations
}

func (r *RayServiceReconciler) createRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1.RayService) (*rayv1.RayCluster, error) {
	logger := ctrl.LoggerFrom(ctx)
	rayClusterKey := common.RayServicePendingRayClusterNamespacedName(rayServiceInstance)
	logger.Info("createRayClusterInstance", "clusterName", rayClusterKey.Name)

	rayClusterInstance, err := constructRayClusterForRayService(rayServiceInstance, rayClusterKey.Name, r.Scheme)
	if err != nil {
		return nil, err
	}
	if err = r.Create(ctx, rayClusterInstance); err != nil {
		logger.Error(err, "Failed to create the RayCluster")
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToCreateRayCluster), "Failed to create the RayCluster %s/%s: %v", rayClusterInstance.Namespace, rayClusterInstance.Name, err)
		return nil, err
	}
	logger.Info("Created RayCluster for RayService", "clusterName", rayClusterInstance.Name)
	r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.CreatedRayCluster), "Created the RayCluster %s/%s", rayClusterInstance.Namespace, rayClusterInstance.Name)
	return rayClusterInstance, nil
}

func constructRayClusterForRayService(rayService *rayv1.RayService, rayClusterName string, scheme *runtime.Scheme) (*rayv1.RayCluster, error) {
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
	rayClusterAnnotations[utils.HashWithoutReplicasAndWorkersToDeleteKey], err = generateHashWithoutReplicasAndWorkersToDelete(rayService.Spec.RayClusterSpec)
	if err != nil {
		return nil, err
	}
	rayClusterAnnotations[utils.NumWorkerGroupsKey] = strconv.Itoa(len(rayService.Spec.RayClusterSpec.WorkerGroupSpecs))

	// set the KubeRay version used to create the RayCluster
	rayClusterAnnotations[utils.KubeRayVersion] = utils.KUBERAY_VERSION

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
	if err := ctrl.SetControllerReference(rayService, rayCluster, scheme); err != nil {
		return nil, err
	}

	return rayCluster, nil
}

func checkIfNeedSubmitServeApplications(cachedServeConfigV2 string, serveConfigV2 string, serveApplications map[string]rayv1.AppStatus) (bool, string) {
	// If the Serve config has not been cached, update the Serve config.
	if cachedServeConfigV2 == "" {
		return true, "Nothing has been cached for the cluster."
	}

	// Handle the case that the head Pod has crashed and GCS FT is not enabled.
	if len(serveApplications) == 0 {
		reason := "No Serve application found in the RayCluster. " +
			"A possible reason is that the head Pod crashed and GCS FT was not enabled."
		return true, reason
	}

	// If the Serve config has been cached, check if it needs to be updated.
	if cachedServeConfigV2 != serveConfigV2 {
		return true, "Current V2 Serve config doesn't match cached Serve config."
	}

	return false, "Current V2 Serve config matches cached Serve config."
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
		return fmt.Errorf("failed to marshal converted serve config into bytes: %w", err)
	}
	logger.Info("updateServeDeployment", "MULTI_APP json config", string(configJson))
	if err := rayDashboardClient.UpdateDeployments(ctx, configJson); err != nil {
		err = fmt.Errorf(
			"fail to create / update Serve applications. If you observe this error consistently, "+
				"please check \"Issue 5: Fail to create / update Serve applications.\" in "+
				"https://docs.ray.io/en/master/cluster/kubernetes/troubleshooting/rayservice-troubleshooting.html#kuberay-raysvc-troubleshoot for more details. "+
				"err: %v", err)
		return err
	}

	r.cacheServeConfig(rayServiceInstance, clusterName)
	logger.Info("updateServeDeployment", "message", "Cached Serve config for Ray cluster with the key", "rayClusterName", clusterName)
	return nil
}

// `getAndCheckServeStatus` gets Serve applications' and deployments' statuses and check whether the
// Serve applications are ready to serve incoming traffic or not. It returns three values:
//
// (1) `isReady`: Whether the Serve applications are ready to serve incoming traffic or not.
// (2) `newApplications`: The Serve applications' statuses.
// (3) `err`: If `err` is not nil, it means that KubeRay failed to get Serve application statuses from the dashboard.
func getAndCheckServeStatus(ctx context.Context, dashboardClient utils.RayDashboardClientInterface) (bool, map[string]rayv1.AppStatus, error) {
	logger := ctrl.LoggerFrom(ctx)
	var serveAppStatuses map[string]*utils.ServeApplicationStatus
	var err error
	if serveAppStatuses, err = dashboardClient.GetMultiApplicationStatus(ctx); err != nil {
		err = fmt.Errorf(
			"failed to get Serve application statuses from the dashboard. "+
				"If you observe this error consistently, please check https://docs.ray.io/en/latest/cluster/kubernetes/troubleshooting/rayservice-troubleshooting.html for more details. "+
				"err: %v", err)
		return false, nil, err
	}

	isReady := true

	newApplications := make(map[string]rayv1.AppStatus)
	for appName, app := range serveAppStatuses {
		if appName == "" {
			appName = utils.DefaultServeAppName
		}

		applicationStatus := rayv1.AppStatus{
			Message:     app.Message,
			Status:      app.Status,
			Deployments: make(map[string]rayv1.ServeDeploymentStatus),
		}

		// `isReady` is used to determine whether the Serve application is ready or not. The cluster switchover only happens when all Serve
		// applications in this RayCluster are ready so that the incoming traffic will not be dropped.
		if app.Status != rayv1.ApplicationStatusEnum.RUNNING {
			isReady = false
		}

		// Copy deployment statuses
		for deploymentName, deployment := range app.Deployments {
			deploymentStatus := rayv1.ServeDeploymentStatus{
				Status:  deployment.Status,
				Message: deployment.Message,
			}
			applicationStatus.Deployments[deploymentName] = deploymentStatus
		}
		newApplications[appName] = applicationStatus
	}

	if len(newApplications) == 0 {
		logger.Info("No Serve application found. The RayCluster is not ready to serve requests. Set 'isReady' to false")
		isReady = false
	}
	return isReady, newApplications, nil
}

func (r *RayServiceReconciler) getServeConfigFromCache(rayServiceInstance *rayv1.RayService, clusterName string) string {
	cacheKey := rayServiceInstance.Namespace + "/" + rayServiceInstance.Name
	cacheValue, exist := r.ServeConfigs.Get(cacheKey)
	if !exist {
		return ""
	}
	serveConfigs := cacheValue.(cmap.ConcurrentMap[string, string])
	serveConfig, exist := serveConfigs.Get(clusterName)
	if !exist {
		return ""
	}
	return serveConfig
}

func (r *RayServiceReconciler) cacheServeConfig(rayServiceInstance *rayv1.RayService, clusterName string) {
	serveConfig := rayServiceInstance.Spec.ServeConfigV2
	if serveConfig == "" {
		return
	}
	cacheKey := rayServiceInstance.Namespace + "/" + rayServiceInstance.Name
	cacheValue, exist := r.ServeConfigs.Get(cacheKey)
	var rayServiceServeConfigs cmap.ConcurrentMap[string, string]
	if !exist {
		rayServiceServeConfigs = cmap.New[string]()
		r.ServeConfigs.Add(cacheKey, rayServiceServeConfigs)
	} else {
		rayServiceServeConfigs = cacheValue.(cmap.ConcurrentMap[string, string])
	}
	rayServiceServeConfigs.Set(clusterName, serveConfig)
}

func (r *RayServiceReconciler) reconcileServices(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster, serviceType utils.ServiceType) (*corev1.Service, error) {
	logger := ctrl.LoggerFrom(ctx)

	var newSvc *corev1.Service
	var err error

	switch serviceType {
	case utils.HeadService:
		newSvc, err = common.BuildHeadServiceForRayService(ctx, *rayServiceInstance, *rayClusterInstance)
	case utils.ServingService:
		newSvc, err = common.BuildServeServiceForRayService(ctx, *rayServiceInstance, *rayClusterInstance)
	default:
		panic(fmt.Sprintf("unknown service type %v. This should never happen. Please open an issue in the KubeRay repository.", serviceType))
	}

	if err != nil {
		return nil, err
	}

	// Retrieve the Service from the Kubernetes cluster with the name and namespace.
	oldSvc := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Name: newSvc.Name, Namespace: rayServiceInstance.Namespace}, oldSvc)

	if err == nil {
		// Only update the service if the RayCluster switches.
		if newSvc.Spec.Selector[utils.RayClusterLabelKey] == oldSvc.Spec.Selector[utils.RayClusterLabelKey] {
			logger.Info("Service has already exists in the RayCluster, skip Update", "rayCluster", newSvc.Spec.Selector[utils.RayClusterLabelKey], "serviceType", serviceType)
			return oldSvc, nil
		}

		// ClusterIP is immutable. Starting from Kubernetes v1.21.5, if the new service does not specify a ClusterIP,
		// Kubernetes will assign the ClusterIP of the old service to the new one. However, to maintain compatibility
		// with older versions of Kubernetes, we need to assign the ClusterIP here.
		newSvc.Spec.ClusterIP = oldSvc.Spec.ClusterIP
		oldSvc.Spec = *newSvc.Spec.DeepCopy()
		logger.Info("Update Kubernetes Service", "serviceType", serviceType)
		if updateErr := r.Update(ctx, oldSvc); updateErr != nil {
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToUpdateService), "Failed to update the service %s/%s, %v", oldSvc.Namespace, oldSvc.Name, updateErr)
			return nil, updateErr
		}
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedService), "Updated the service %s/%s", oldSvc.Namespace, oldSvc.Name)
		// Return the updated service.
		return oldSvc, nil
	} else if errors.IsNotFound(err) {
		logger.Info("Create a Kubernetes Service", "serviceType", serviceType)
		if err := ctrl.SetControllerReference(rayServiceInstance, newSvc, r.Scheme); err != nil {
			return nil, err
		}
		if err := r.Create(ctx, newSvc); err != nil {
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToCreateService), "Failed to create the service %s/%s, %v", newSvc.Namespace, newSvc.Name, err)
			return nil, err
		}
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.CreatedService), "Created the service %s/%s", newSvc.Namespace, newSvc.Name)
		return newSvc, nil
	}
	return nil, err
}

// Reconciles the Serve applications on the RayCluster. Returns (isReady, error).
// The `isReady` flag indicates whether the RayCluster is ready to handle incoming traffic.
func (r *RayServiceReconciler) reconcileServe(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster) (bool, map[string]rayv1.AppStatus, error) {
	logger := ctrl.LoggerFrom(ctx)
	var err error
	var serveApplications map[string]rayv1.AppStatus
	// Check if head pod is running and ready. If not, requeue the resource event to avoid
	// redundant custom resource status updates.
	//
	// TODO (kevin85421): Note that the Dashboard and GCS may take a few seconds to start up
	// after the head pod is running and ready. Hence, some requests to the Dashboard (e.g. `UpdateDeployments`) may fail.
	// This is not an issue since `UpdateDeployments` is an idempotent operation.
	if features.Enabled(features.RayClusterStatusConditions) {
		if !meta.IsStatusConditionTrue(rayClusterInstance.Status.Conditions, string(rayv1.HeadPodReady)) {
			logger.Info("The head Pod is not ready, requeue the resource event to avoid redundant custom resource status updates.")
			return false, serveApplications, nil
		}
	} else {
		if isRunningAndReady, err := r.isHeadPodRunningAndReady(ctx, rayClusterInstance); err != nil || !isRunningAndReady {
			if err != nil {
				logger.Error(err, "Failed to check if head Pod is running and ready!")
			} else {
				logger.Info("Skipping the update of Serve applications because the Ray head Pod is not ready.")
			}
			return false, serveApplications, nil
		}
	}

	var clientURL string
	if clientURL, err = utils.FetchHeadServiceURL(ctx, r.Client, rayClusterInstance, utils.DashboardPortName); err != nil || clientURL == "" {
		return false, serveApplications, err
	}

	rayDashboardClient := r.dashboardClientFunc()
	if err := rayDashboardClient.InitClient(ctx, clientURL, rayClusterInstance); err != nil {
		return false, serveApplications, err
	}

	cachedServeConfigV2 := r.getServeConfigFromCache(rayServiceInstance, rayClusterInstance.Name)
	isReady, serveApplications, err := getAndCheckServeStatus(ctx, rayDashboardClient)
	if err != nil {
		return false, serveApplications, err
	}
	shouldUpdate, reason := checkIfNeedSubmitServeApplications(cachedServeConfigV2, rayServiceInstance.Spec.ServeConfigV2, serveApplications)
	logger.Info("checkIfNeedSubmitServeApplications", "shouldUpdate", shouldUpdate, "reason", reason)

	if shouldUpdate {
		if err = r.updateServeDeployment(ctx, rayServiceInstance, rayDashboardClient, rayClusterInstance.Name); err != nil {
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToUpdateServeApplications), "Failed to update serve applications to the RayCluster %s/%s: %v", rayClusterInstance.Namespace, rayClusterInstance.Name, err)
			return false, serveApplications, err
		}
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedServeApplications), "Updated serve applications to the RayCluster %s/%s", rayClusterInstance.Namespace, rayClusterInstance.Name)
	}
	return isReady, serveApplications, nil
}

func (r *RayServiceReconciler) updateHeadPodServeLabel(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster, excludeHeadPodFromServeSvc bool) error {
	// `updateHeadPodServeLabel` updates the head Pod's serve label based on the health status of the proxy actor.
	// If `excludeHeadPodFromServeSvc` is true, the head Pod will not be used to serve requests, regardless of proxy actor health.
	// If `excludeHeadPodFromServeSvc` is false, the head Pod's serve label will be set based on the health check result.
	// The label is used by the Kubernetes serve service to determine whether to include the head Pod in the service endpoints.
	if rayClusterInstance == nil {
		return nil
	}

	headPod, err := common.GetRayClusterHeadPod(ctx, r, rayClusterInstance)
	if err != nil {
		return err
	}
	if headPod == nil {
		return fmt.Errorf("found 0 head. cluster name %s, namespace %v", rayClusterInstance.Name, rayClusterInstance.Namespace)
	}

	client := r.httpProxyClientFunc()
	client.InitClient()

	rayContainer := headPod.Spec.Containers[utils.RayContainerIndex]
	servingPort := utils.FindContainerPort(&rayContainer, utils.ServingPortName, utils.DefaultServingPort)
	client.SetHostIp(headPod.Status.PodIP, headPod.Namespace, headPod.Name, servingPort)

	if headPod.Labels == nil {
		headPod.Labels = make(map[string]string)
	}
	oldLabel := headPod.Labels[utils.RayClusterServingServiceLabelKey]
	newLabel := utils.EnableRayClusterServingServiceFalse

	// If excludeHeadPodFromServeSvc is true, head Pod will not be used to serve requests
	// no matter whether the proxy actor is healthy or not. Therefore, only send the health
	// check request if excludeHeadPodFromServeSvc is false.
	if !excludeHeadPodFromServeSvc {
		isHealthy := client.CheckProxyActorHealth(ctx) == nil
		newLabel = strconv.FormatBool(isHealthy)
	}

	if oldLabel != newLabel {
		headPod.Labels[utils.RayClusterServingServiceLabelKey] = newLabel
		if updateErr := r.Update(ctx, headPod); updateErr != nil {
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToUpdateHeadPodServeLabel), "Failed to update the serve label to %q for the Head Pod %s/%s: %v", newLabel, headPod.Namespace, headPod.Name, updateErr)
			return updateErr
		}
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedHeadPodServeLabel), "Updated the serve label to %q for the Head Pod %s/%s", newLabel, headPod.Namespace, headPod.Name)
	}

	return nil
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

// isHeadPodRunningAndReady checks if the head pod of the RayCluster is running and ready.
func (r *RayServiceReconciler) isHeadPodRunningAndReady(ctx context.Context, instance *rayv1.RayCluster) (bool, error) {
	headPod, err := common.GetRayClusterHeadPod(ctx, r, instance)
	if err != nil {
		return false, err
	}
	if headPod == nil {
		return false, fmt.Errorf("found 0 head. cluster name %s, namespace %v", instance.Name, instance.Namespace)
	}
	return utils.IsRunningAndReady(headPod), nil
}
