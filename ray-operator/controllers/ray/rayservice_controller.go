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

	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/lru"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/dashboardclient"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
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
	dashboardClientFunc          func(rayCluster *rayv1.RayCluster, url string) (dashboardclient.RayDashboardClientInterface, error)
	httpProxyClientFunc          func(hostIp, podNamespace, podName string, port int) utils.RayHttpProxyClientInterface
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
// +kubebuilder:rbac:groups="gateway.networking.k8s.io",resources=gateways,verbs=get;create;update;
// +kubebuilder:rbac:groups="gateway.networking.k8s.io",resources=httproutes,verbs=get;create;update;
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

	// Check if IncrementalUpgrade is enabled, if so reconcile Gateway objects.
	if utils.IsIncrementalUpgradeEnabled(&rayServiceInstance.Spec) {
		// Creates a Gateway CR that points to the head services of
		// the active and pending (if it exists) RayClusters. For incremental upgrades,
		// the Gateway endpoint is used rather than the Serve service.
		gateway, err := r.reconcileGateway(ctx, rayServiceInstance)
		if err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, client.IgnoreNotFound(err)
		}
		if gateway != nil {
			rayServiceInstance.Spec.Gateway = gateway.Name
		}
		// Create or update the HTTPRoute attached to this RayService's Gateway
		httpRoute, err := r.reconcileHTTPRoute(ctx, rayServiceInstance)
		if err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, client.IgnoreNotFound(err)
		}
		if httpRoute != nil {
			rayServiceInstance.Spec.HTTPRoute = httpRoute.Name
		}
	}

	// Reconcile serve applications for active and/or pending clusters
	// 1. If there is a pending cluster, reconcile serve applications for the pending cluster.
	// 2. If there are both active and pending clusters, reconcile serve applications for the pending cluster only.
	// 3. If there is no pending cluster, reconcile serve applications for the active cluster.
	// 4. During an IncrementalUpgrade, reconcileServe will reconcile either the pending or active cluster based
	//    on total TargetCapacity.
	var isActiveClusterReady, isPendingClusterReady bool = false, false
	var activeClusterServeApplications, pendingClusterServeApplications map[string]rayv1.AppStatus = nil, nil
	if pendingRayClusterInstance != nil {
		logger.Info("Reconciling the Serve applications for pending cluster", "clusterName", pendingRayClusterInstance.Name)
		if isPendingClusterReady, pendingClusterServeApplications, err = r.reconcileServe(ctx, rayServiceInstance, pendingRayClusterInstance); err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
	}
	if activeRayClusterInstance != nil && pendingRayClusterInstance == nil &&
		!shouldPrepareNewCluster(ctx, rayServiceInstance, activeRayClusterInstance, nil, false) {
		// Only reconcile serve applications for the active cluster when there is no pending cluster. That is, during the upgrade process,
		// in-place update and updating the serve application status for the active cluster will not work.
		logger.Info("Reconciling the Serve applications for active cluster", "clusterName", activeRayClusterInstance.Name)
		if isActiveClusterReady, activeClusterServeApplications, err = r.reconcileServe(ctx, rayServiceInstance, activeRayClusterInstance); err != nil {
			return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, err
		}
	} else if activeRayClusterInstance != nil && utils.IsIncrementalUpgradeEnabled(&rayServiceInstance.Spec) {
		logger.Info("Reconciling the Serve applications for active cluster during IncrementalUpgrade", "clusterName", activeRayClusterInstance.Name)
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
	if utils.InconsistentRayServiceStatuses(originalRayServiceInstance.Status, rayServiceInstance.Status) {
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

		if utils.IsIncrementalUpgradeEnabled(&rayServiceInstance.Spec) {
			// Set IncrementalUpgrade related Status fields for new pending RayCluster if enabled
			if rayServiceInstance.Status.ActiveServiceStatus.RayClusterName == "" {
				// If no Active RayCluster exists - default to starting with 100% TargetCapacity.
				if rayServiceInstance.Status.ActiveServiceStatus.TargetCapacity == nil {
					rayServiceInstance.Status.PendingServiceStatus.TargetCapacity = ptr.To(int32(100))
				}
			} else if meta.IsStatusConditionTrue(rayServiceInstance.Status.Conditions, string(rayv1.UpgradeInProgress)) {
				// Pending RayCluster during an upgrade should start with 0% TargetCapacity.
				if rayServiceInstance.Status.PendingServiceStatus.TargetCapacity == nil {
					rayServiceInstance.Status.PendingServiceStatus.TargetCapacity = ptr.To(int32(0))
				}
			}
		}
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
			if features.Enabled(features.RayServiceIncrementalUpgrade) {
				if *upgradeType != rayv1.NewCluster && *upgradeType != rayv1.IncrementalUpgrade {
					logger.Info("Zero-downtime upgrade is disabled because UpgradeStrategy.Type is not set to %s or %s.", string(rayv1.NewCluster), string(rayv1.IncrementalUpgrade))
					return false
				}
			} else if *upgradeType != rayv1.NewCluster {
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

func (r *RayServiceReconciler) createGateway(rayServiceInstance *rayv1.RayService) (*gwv1.Gateway, error) {
	options := utils.GetRayServiceIncrementalUpgradeOptions(&rayServiceInstance.Spec)
	if options == nil {
		return nil, errstd.New("Missing RayService IncrementalUpgradeOptions during upgrade")
	}

	var gatewayName string
	if rayServiceInstance.Spec.Gateway != "" {
		gatewayName = rayServiceInstance.Spec.Gateway
	} else {
		gatewayName = rayServiceInstance.Name + "-gateway"
	}
	gatewayName = utils.CheckGatewayName(gatewayName)
	// Define the desired Gateway object
	rayServiceGateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: rayServiceInstance.Namespace,
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: gwv1.ObjectName(options.GatewayClassName),
		},
	}

	rayServiceGateway.Spec.Listeners = utils.GetGatewayListenersForRayService(rayServiceInstance)

	return rayServiceGateway, nil
}

// `reconcileGateway` reconciles a Gateway resource for a RayService. The possible cases are:
// (1) Create a new Gateway instance. (2) Update the Gateway instance if RayService has updated. (3) Do nothing.
func (r *RayServiceReconciler) reconcileGateway(ctx context.Context, rayServiceInstance *rayv1.RayService) (*gwv1.Gateway, error) {
	logger := ctrl.LoggerFrom(ctx)
	var err error

	// Construct desired Gateway object for RayService
	desiredGateway, err := r.createGateway(rayServiceInstance)
	if err != nil {
		logger.Error(err, "Failed to build Gateway object for Rayservice")
		return nil, err
	}
	if desiredGateway == nil {
		logger.Info("Skipping Gateway reconciliation: desired Gateway is nil")
		return nil, nil
	}

	// Check for existing RayService Gateway, create the desired Gateway if none is found
	existingGateway := &gwv1.Gateway{}
	if err := r.Get(ctx, common.RayServiceGatewayNamespacedName(rayServiceInstance), existingGateway); err != nil {
		if errors.IsNotFound(err) {
			// Set the ownership in order to do the garbage collection by k8s.
			if err := ctrl.SetControllerReference(rayServiceInstance, desiredGateway, r.Scheme); err != nil {
				return nil, err
			}
			logger.Info("Creating a new Gateway instance", "Gateway Listeners", desiredGateway.Spec.Listeners)
			if err := r.Create(ctx, desiredGateway); err != nil {
				r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToCreateGateway), "Failed to create Gateway for RayService %s/%s: %v", desiredGateway.Namespace, desiredGateway.Name, err)
				return nil, err
			}
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedRayCluster), "Created Gateway for RayService %s/%s", desiredGateway.Namespace, desiredGateway.Name)
			return desiredGateway, nil
		}
		return nil, err
	}

	// If Gateway already exists, check if update is needed to reach desired state
	if !reflect.DeepEqual(existingGateway.Spec, desiredGateway.Spec) {
		logger.Info("Updating existing Gateway", "name", existingGateway.Name)
		existingGateway.Spec = desiredGateway.Spec
		if err := r.Update(ctx, existingGateway); err != nil {
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToUpdateGateway), "Failed to update the Gateway %s/%s: %v", existingGateway.Namespace, existingGateway.Name, err)
			return nil, err
		}
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedGateway), "Updated the Gateway %s/%s", existingGateway.Namespace, existingGateway.Name)
	}

	return existingGateway, nil
}

// createHTTPRoute creates a desired HTTPRoute object based on a given RayService instance with
// weights based on TrafficRoutedPercent.
func (r *RayServiceReconciler) createHTTPRoute(ctx context.Context, rayServiceInstance *rayv1.RayService) (*gwv1.HTTPRoute, error) {
	logger := ctrl.LoggerFrom(ctx)

	// Retrieve Gateway instance to attach this HTTPRoute to
	gatewayInstance := &gwv1.Gateway{}
	if err := r.Get(ctx, common.RayServiceGatewayNamespacedName(rayServiceInstance), gatewayInstance); err != nil {
		return nil, err
	}

	// Define the desired HTTPRoute name and basic object
	var httpRouteName string
	if rayServiceInstance.Spec.HTTPRoute != "" {
		httpRouteName = rayServiceInstance.Spec.HTTPRoute
	} else {
		httpRouteName = fmt.Sprintf("httproute-%s", rayServiceInstance.Name)
	}
	httpRouteName = utils.CheckHTTPRouteName(httpRouteName)
	desiredHTTPRoute := &gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpRouteName,
			Namespace: rayServiceInstance.Namespace,
		},
		Spec: gwv1.HTTPRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{
				ParentRefs: []gwv1.ParentReference{
					{
						Name:      gwv1.ObjectName(gatewayInstance.Name),
						Namespace: ptr.To(gwv1.Namespace(gatewayInstance.Namespace)),
					},
				},
			},
		},
	}

	// Retrieve the active RayCluster
	activeRayCluster, err := r.getRayClusterByNamespacedName(ctx, common.RayServiceActiveRayClusterNamespacedName(rayServiceInstance))
	if err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "Failed to retrieve active RayCluster")
		return nil, err
	}
	if activeRayCluster == nil || activeRayCluster.Status.Head.ServiceName == "" {
		logger.Info("Active RayCluster not found, skipping HTTPRoute creation.")
		return nil, nil
	}
	oldClusterHeadSvcName := activeRayCluster.Status.Head.ServiceName
	oldHeadSvc := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Name: oldClusterHeadSvcName, Namespace: rayServiceInstance.Namespace}, oldHeadSvc); err != nil {
		logger.Error(err, "Failed to retrieve active RayCluster head service")
		return nil, err
	}

	// Attempt to retrieve pending RayCluster
	pendingRayCluster, err := r.getRayClusterByNamespacedName(ctx, common.RayServicePendingRayClusterNamespacedName(rayServiceInstance))
	hasPendingCluster := (err == nil && pendingRayCluster != nil && pendingRayCluster.Status.Head.ServiceName != "")
	if err != nil && !errors.IsNotFound(err) {
		logger.Info("Failed to retrieve pending RayCluster.")
	}

	activeServiceStatus := rayServiceInstance.Status.ActiveServiceStatus

	var backendRefs []gwv1.HTTPBackendRef

	// Configure HTTPRoute to split traffic between active and pending clusters during an incremental upgrade
	if hasPendingCluster {
		newClusterHeadSvcName := pendingRayCluster.Status.Head.ServiceName
		newHeadSvc := &corev1.Service{}
		if err := r.Get(ctx, client.ObjectKey{Name: newClusterHeadSvcName, Namespace: rayServiceInstance.Namespace}, newHeadSvc); err != nil {
			logger.Error(err, "Failed to retrieve pending RayCluster head service")
			return nil, err
		}

		options := utils.GetRayServiceIncrementalUpgradeOptions(&rayServiceInstance.Spec)
		if options == nil {
			return nil, errstd.New("Missing RayService IncrementalUpgradeOptions")
		}

		// Retrieve TrafficRoutedPercent for old and upgraded RayClusters.
		pendingServiceStatus := rayServiceInstance.Status.PendingServiceStatus
		newClusterWeight := pendingServiceStatus.TrafficRoutedPercent
		oldClusterWeight := activeServiceStatus.TrafficRoutedPercent

		// If IntervalSeconds has passed since LastTrafficMigratedTime, migrate
		// StepSizePercent traffic to the pending cluster.
		intervalSeconds := time.Duration(*options.IntervalSeconds) * time.Second
		lastTrafficMigratedTime := pendingServiceStatus.LastTrafficMigratedTime
		if (newClusterWeight != nil && oldClusterWeight != nil) && (lastTrafficMigratedTime == nil || time.Since(lastTrafficMigratedTime.Time) >= intervalSeconds) {
			// Wait an initial iteration before migrating StepSizePercent.
			if lastTrafficMigratedTime != nil {
				logger.Info("Updating cluster weights by StepSizePercent each")
				oldClusterWeight = ptr.To(max(*oldClusterWeight-*options.StepSizePercent, 0))
				newClusterWeight = ptr.To(min(*newClusterWeight+*options.StepSizePercent, 100))
			}
			rayServiceInstance.Status.PendingServiceStatus.LastTrafficMigratedTime = &metav1.Time{Time: time.Now()}
			rayServiceInstance.Status.ActiveServiceStatus.LastTrafficMigratedTime = &metav1.Time{Time: time.Now()}
		}

		// Set weights for initial iteration.
		if newClusterWeight == nil {
			// Pending RayCluster should scale up from 0 TrafficRoutedPercent.
			newClusterWeight = ptr.To(int32(0))
		}
		if oldClusterWeight == nil {
			// Active RayCluster should scale down from 100 TrafficRoutedPercent.
			oldClusterWeight = ptr.To(int32(100))
		}
		// HTTPRoute weights should never exceed current TargetCapacity for each cluster.
		newClusterTargetCapacity := pendingServiceStatus.TargetCapacity
		oldClusterTargetCapacity := activeServiceStatus.TargetCapacity
		if newClusterTargetCapacity != nil {
			newClusterWeight = ptr.To(min(*newClusterWeight, *newClusterTargetCapacity))
		}
		if oldClusterTargetCapacity != nil {
			oldClusterWeight = ptr.To(min(*oldClusterWeight, *oldClusterTargetCapacity))
		}

		backendRefs = []gwv1.HTTPBackendRef{
			{
				BackendRef: gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name:      gwv1.ObjectName(oldClusterHeadSvcName),
						Namespace: ptr.To(gwv1.Namespace(rayServiceInstance.Namespace)),
						Port:      ptr.To(gwv1.PortNumber(8000)), // set to Serve port
					},
					Weight: oldClusterWeight,
				},
			},
			{
				BackendRef: gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name:      gwv1.ObjectName(newClusterHeadSvcName),
						Namespace: ptr.To(gwv1.Namespace(rayServiceInstance.Namespace)),
						Port:      ptr.To(gwv1.PortNumber(8000)),
					},
					Weight: newClusterWeight,
				},
			},
		}
		logger.Info("Updating TrafficRoutedPercent to", "oldClusterWeight", oldClusterWeight, "newClusterWeight", newClusterWeight)
		rayServiceInstance.Status.ActiveServiceStatus.TrafficRoutedPercent = oldClusterWeight
		rayServiceInstance.Status.PendingServiceStatus.TrafficRoutedPercent = newClusterWeight
	} else {
		// No pending cluster — route 100% to active RayCluster
		backendRefs = []gwv1.HTTPBackendRef{
			{
				BackendRef: gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name:      gwv1.ObjectName(oldClusterHeadSvcName),
						Namespace: ptr.To(gwv1.Namespace(rayServiceInstance.Namespace)),
						Port:      ptr.To(gwv1.PortNumber(8000)),
					},
					Weight: ptr.To(int32(100)),
				},
			},
		}
		rayServiceInstance.Status.ActiveServiceStatus.TrafficRoutedPercent = ptr.To(int32(100))
	}

	desiredHTTPRoute.Spec.Rules = []gwv1.HTTPRouteRule{
		{
			Matches: []gwv1.HTTPRouteMatch{
				{
					Path: &gwv1.HTTPPathMatch{
						Type:  ptr.To(gwv1.PathMatchPathPrefix),
						Value: ptr.To("/"),
					},
				},
			},
			BackendRefs: backendRefs,
		},
	}

	return desiredHTTPRoute, nil
}

// reconcileHTTPRoute reconciles a HTTPRoute resource for a RayService to route traffic during an IncrementalUpgrade.
func (r *RayServiceReconciler) reconcileHTTPRoute(ctx context.Context, rayServiceInstance *rayv1.RayService) (*gwv1.HTTPRoute, error) {
	logger := ctrl.LoggerFrom(ctx)
	var err error

	desiredHTTPRoute, err := r.createHTTPRoute(ctx, rayServiceInstance)
	if err != nil {
		logger.Error(err, "Failed to build HTTPRoute for RayService upgrade")
		return nil, err
	}
	if desiredHTTPRoute == nil {
		logger.Info("Skipping HTTPRoute reconciliation: desired HTTPRoute is nil")
		return nil, nil
	}

	// Check for existing HTTPRoute for RayService
	existingHTTPRoute := &gwv1.HTTPRoute{}
	if err := r.Get(ctx, common.RayServiceHTTPRouteNamespacedName(rayServiceInstance), existingHTTPRoute); err != nil {
		if errors.IsNotFound(err) {
			// Set the ownership in order to do the garbage collection by k8s.
			if err := ctrl.SetControllerReference(rayServiceInstance, desiredHTTPRoute, r.Scheme); err != nil {
				return nil, err
			}
			if err = r.Create(ctx, desiredHTTPRoute); err != nil {
				r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToCreateHTTPRoute), "Failed to create the HTTPRoute for RayService %s/%s: %v", desiredHTTPRoute.Namespace, desiredHTTPRoute.Name, err)
				return nil, err
			}
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.FailedToCreateHTTPRoute), "Created HTTPRoute for RayService %s/%s", desiredHTTPRoute.Namespace, desiredHTTPRoute.Name)
			return desiredHTTPRoute, nil
		}
		return nil, err
	}

	// If HTTPRoute already exists, check if update is needed
	if !reflect.DeepEqual(existingHTTPRoute.Spec, desiredHTTPRoute.Spec) {
		logger.Info("Updating existing HTTPRoute", "name", desiredHTTPRoute.Name)
		existingHTTPRoute.Spec = desiredHTTPRoute.Spec
		if err := r.Update(ctx, existingHTTPRoute); err != nil {
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToUpdateHTTPRoute), "Failed to update the HTTPRoute %s/%s: %v", existingHTTPRoute.Namespace, existingHTTPRoute.Name, err)
			return nil, err
		}
		r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedHTTPRoute), "Updated the HTTPRoute %s/%s", existingHTTPRoute.Namespace, existingHTTPRoute.Name)
	}

	return existingHTTPRoute, nil
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

	// Determine the ray cluster deletion delay seconds
	deletionDelay := RayClusterDeletionDelayDuration
	if rayServiceInstance.Spec.RayClusterDeletionDelaySeconds != nil {
		deletionDelay = time.Duration(*rayServiceInstance.Spec.RayClusterDeletionDelaySeconds) * time.Second
	}
	// Clean up RayCluster instances. Each instance is deleted after the configured deletion delay.
	for _, rayClusterInstance := range rayClusterList.Items {
		if rayClusterInstance.Name != rayServiceInstance.Status.ActiveServiceStatus.RayClusterName && rayClusterInstance.Name != rayServiceInstance.Status.PendingServiceStatus.RayClusterName {
			cachedTimestamp, exists := r.RayClusterDeletionTimestamps.Get(rayClusterInstance.Name)
			if !exists {
				deletionTimestamp := metav1.Now().Add(deletionDelay)
				r.RayClusterDeletionTimestamps.Set(rayClusterInstance.Name, deletionTimestamp)
				logger.Info(
					"Scheduled dangling RayCluster for deletion",
					"rayClusterName", rayClusterInstance.Name,
					"deletionDelay", deletionDelay.String(),
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

func (r *RayServiceReconciler) updateServeDeployment(ctx context.Context, rayServiceInstance *rayv1.RayService, rayDashboardClient dashboardclient.RayDashboardClientInterface, clusterName string) error {
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

// checkIfNeedIncrementalUpgradeUpdate returns whether the controller should adjust the target_capacity
// of the Serve config associated with a RayCluster during an IncrementalUpgrade.
func (r *RayServiceReconciler) checkIfNeedIncrementalUpgradeUpdate(ctx context.Context, rayServiceInstance *rayv1.RayService) (bool, string) {
	activeRayServiceStatus := rayServiceInstance.Status.ActiveServiceStatus
	pendingRayServiceStatus := rayServiceInstance.Status.PendingServiceStatus

	if activeRayServiceStatus.RayClusterName == "" || pendingRayServiceStatus.RayClusterName == "" {
		return false, "Both active and pending RayCluster instances required for incremental upgrade."
	}

	// Validate Gateway and HTTPRoute objects are ready
	gatewayInstance := &gwv1.Gateway{}
	if err := r.Get(ctx, common.RayServiceGatewayNamespacedName(rayServiceInstance), gatewayInstance); err != nil {
		return false, fmt.Sprintf("Failed to retrieve Gateway for RayService: %v", err)
	}
	if !utils.IsGatewayReady(gatewayInstance) {
		return false, "Gateway for RayService IncrementalUpgrade is not ready."
	}

	httpRouteInstance := &gwv1.HTTPRoute{}
	if err := r.Get(ctx, common.RayServiceHTTPRouteNamespacedName(rayServiceInstance), httpRouteInstance); err != nil {
		return false, fmt.Sprintf("Failed to retrieve HTTPRoute for RayService: %v", err)
	}
	if !utils.IsHTTPRouteReady(gatewayInstance, httpRouteInstance) {
		return false, "HTTPRoute for RayService IncrementalUpgrade is not ready."
	}

	// Retrieve the current observed IncrementalUpgrade Status fields for each RayService.
	if activeRayServiceStatus.TargetCapacity == nil || activeRayServiceStatus.TrafficRoutedPercent == nil {
		return true, "Active RayServiceStatus missing TargetCapacity or TrafficRoutedPercent."
	}
	if pendingRayServiceStatus.TargetCapacity == nil || pendingRayServiceStatus.TrafficRoutedPercent == nil {
		return true, "Pending RayServiceStatus missing TargetCapacity or TrafficRoutedPercent."
	}
	activeTargetCapacity := int(*activeRayServiceStatus.TargetCapacity)
	pendingTargetCapacity := int(*pendingRayServiceStatus.TargetCapacity)
	pendingTrafficRoutedPercent := int(*pendingRayServiceStatus.TrafficRoutedPercent)

	if pendingTargetCapacity < 100 || pendingTrafficRoutedPercent < 100 {
		return true, "Pending RayCluster has not finished scaling up."
	} else if activeTargetCapacity == 0 && pendingTargetCapacity == 100 {
		return false, "All traffic has migrated to the upgraded cluster and IncrementalUpgrade is complete."
	}
	return true, "Active RayCluster TargetCapacity has not finished scaling down."
}

// reconcileServeTargetCapacity reconciles the target_capacity of the ServeConfig for a given RayCluster during
// an IncrementalUpgrade while also updating the Status.TargetCapacity of the Active and Pending RayServices.
func (r *RayServiceReconciler) reconcileServeTargetCapacity(ctx context.Context, rayServiceInstance *rayv1.RayService, rayClusterInstance *rayv1.RayCluster, rayDashboardClient dashboardclient.RayDashboardClientInterface) error {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("reconcileServeTargetCapacity", "RayService", rayServiceInstance.Name)

	if !utils.IsIncrementalUpgradeEnabled(&rayServiceInstance.Spec) {
		return nil
	}

	activeRayServiceStatus := &rayServiceInstance.Status.ActiveServiceStatus
	pendingRayServiceStatus := &rayServiceInstance.Status.PendingServiceStatus

	// Set initial TargetCapacity values if unset
	if activeRayServiceStatus.TargetCapacity == nil {
		activeRayServiceStatus.TargetCapacity = ptr.To(int32(100))
	}
	if pendingRayServiceStatus.TargetCapacity == nil {
		pendingRayServiceStatus.TargetCapacity = ptr.To(int32(0))
	}

	// Retrieve the current observed Status fields for IncrementalUpgrade
	activeTargetCapacity := *activeRayServiceStatus.TargetCapacity
	pendingTargetCapacity := *pendingRayServiceStatus.TargetCapacity
	pendingTrafficRoutedPercent := *pendingRayServiceStatus.TrafficRoutedPercent

	// Defer updating the target_capacity until traffic weights are updated
	if pendingTargetCapacity != pendingTrafficRoutedPercent {
		logger.Info("Traffic is currently being migrated to pending cluster", "RayCluster", pendingRayServiceStatus.RayClusterName, "TargetCapacity", pendingTargetCapacity, "TrafficRoutedPercent", pendingTrafficRoutedPercent)
		return nil
	}

	// Retrieve MaxSurgePercent - the maximum amount to change TargetCapacity by
	options := utils.GetRayServiceIncrementalUpgradeOptions(&rayServiceInstance.Spec)
	if options == nil {
		return errstd.New("Missing RayService IncrementalUpgradeOptions during upgrade")
	}
	maxSurgePercent := *options.MaxSurgePercent

	// There are two cases:
	// 1. The total target_capacity is greater than 100. This means the pending RayCluster has
	// scaled up traffic and the active RayCluster can be scaled down by MaxSurgePercent.
	// 2. The total target_capacity is equal to 100. This means the pending RayCluster can
	// increase its target_capacity by MaxSurgePercent.
	// If the rayClusterInstance passed into this function is not the cluster to update based
	// on the above conditions, we return without doing anything.
	var clusterName string
	var goalTargetCapacity int32
	if activeTargetCapacity+pendingTargetCapacity > int32(100) {
		// Scale down the Active RayCluster TargetCapacity on this iteration.
		goalTargetCapacity = max(int32(0), activeTargetCapacity-maxSurgePercent)
		clusterName = activeRayServiceStatus.RayClusterName
		if clusterName != rayClusterInstance.Name {
			return nil
		}
		activeRayServiceStatus.TargetCapacity = ptr.To(goalTargetCapacity)
		logger.Info("Setting target_capacity for active Raycluster", "Raycluster", clusterName, "target_capacity", goalTargetCapacity)
	} else {
		// Scale up the Pending RayCluster TargetCapacity on this iteration.
		goalTargetCapacity = min(int32(100), pendingTargetCapacity+maxSurgePercent)
		clusterName = pendingRayServiceStatus.RayClusterName
		if clusterName != rayClusterInstance.Name {
			return nil
		}
		pendingRayServiceStatus.TargetCapacity = ptr.To(goalTargetCapacity)
		logger.Info("Setting target_capacity for pending Raycluster", "Raycluster", clusterName, "target_capacity", goalTargetCapacity)
	}

	// Retrieve cached ServeConfig from last reconciliation for cluster to update
	cachedConfig := r.getServeConfigFromCache(rayServiceInstance, clusterName)
	if cachedConfig == "" {
		cachedConfig = rayServiceInstance.Spec.ServeConfigV2
	}
	logger.Info("Retrieving ServeConfig", "cached", cachedConfig, "ServeConfigV2", rayServiceInstance.Spec.ServeConfigV2)
	serveConfig := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(cachedConfig), &serveConfig); err != nil {
		return err
	}

	// Check if ServeConfig requires update
	if currentTargetCapacity, ok := serveConfig["target_capacity"].(float64); ok {
		if int32(currentTargetCapacity) == goalTargetCapacity {
			logger.Info("target_capacity already updated on RayCluster", "RayCluster", clusterName, "target_capacity", currentTargetCapacity)
			// No update required, return early
			return nil
		}
	}

	// Otherwise, update the target_capacity for the cached ServeConfig
	serveConfig["target_capacity"] = goalTargetCapacity
	configJson, err := json.Marshal(serveConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal converted serve config into bytes: %w", err)
	}
	logger.Info("reconcileServeTargetCapacity", "MULTI_APP json config", string(configJson))
	if err := rayDashboardClient.UpdateDeployments(ctx, configJson); err != nil {
		err = fmt.Errorf(
			"fail to create / update target_capacity for Serve applications. err: %w", err)
		return err
	}

	// Only update the target_capacity of one RayCluster at a time.
	r.cacheServeConfig(rayServiceInstance, clusterName)
	logger.Info("reconcileServeTargetCapacity", "message", "Cached Serve config for Ray cluster with the key", "rayClusterName", clusterName)

	return nil
}

// `getAndCheckServeStatus` gets Serve applications' and deployments' statuses and check whether the
// Serve applications are ready to serve incoming traffic or not. It returns three values:
//
// (1) `isReady`: Whether the Serve applications are ready to serve incoming traffic or not.
// (2) `newApplications`: The Serve applications' statuses.
// (3) `err`: If `err` is not nil, it means that KubeRay failed to get Serve application statuses from the dashboard.
func getAndCheckServeStatus(ctx context.Context, dashboardClient dashboardclient.RayDashboardClientInterface) (bool, map[string]rayv1.AppStatus, error) {
	logger := ctrl.LoggerFrom(ctx)
	var serveAppStatuses map[string]*utiltypes.ServeApplicationStatus
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

// Reconciles the Serve applications on the RayCluster. Returns (isReady, serveApplicationStatus, error).
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

	rayDashboardClient, err := r.dashboardClientFunc(rayClusterInstance, clientURL)
	if err != nil {
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
	if utils.IsIncrementalUpgradeEnabled(&rayServiceInstance.Spec) && meta.IsStatusConditionTrue(rayServiceInstance.Status.Conditions, string(rayv1.UpgradeInProgress)) {
		incrementalUpgradeUpdate, reason := r.checkIfNeedIncrementalUpgradeUpdate(ctx, rayServiceInstance)
		logger.Info("checkIfNeedIncrementalUpgradeUpdate", "incrementalUpgradeUpdate", incrementalUpgradeUpdate, "reason", reason)
		if incrementalUpgradeUpdate {
			if err := r.reconcileServeTargetCapacity(ctx, rayServiceInstance, rayClusterInstance, rayDashboardClient); err != nil {
				r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeWarning, string(utils.FailedToUpdateTargetCapacity), "Failed to update target_capacity of serve applications to the RayCluster %s/%s: %v", rayClusterInstance.Namespace, rayClusterInstance.Name, err)
				return false, serveApplications, err
			}
			r.Recorder.Eventf(rayServiceInstance, corev1.EventTypeNormal, string(utils.UpdatedServeTargetCapacity),
				"Updated target_capacity of serve applications to to the RayCluster %s/%s", rayClusterInstance.Namespace, rayClusterInstance.Name)

			// Don't switch to the pending RayCluster until IncrementalUpgrade is complete.
			if rayServiceInstance.Status.PendingServiceStatus.RayClusterName == rayClusterInstance.Name {
				return false, serveApplications, nil
			}
		}
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

	rayContainer := headPod.Spec.Containers[utils.RayContainerIndex]
	servingPort := utils.FindContainerPort(&rayContainer, utils.ServingPortName, utils.DefaultServingPort)

	client := r.httpProxyClientFunc(headPod.Status.PodIP, headPod.Namespace, headPod.Name, servingPort)
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
