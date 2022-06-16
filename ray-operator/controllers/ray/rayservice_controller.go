package ray

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	networkingv1 "k8s.io/api/networking/v1"

	cmap "github.com/orcaman/concurrent-map"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

var (
	rayServiceLog                           = logf.Log.WithName("rayservice-controller")
	ServeDeploymentUnhealthySecondThreshold = 60.0 // Move to var for unit testing.
)

const (
	ServiceDefaultRequeueDuration     = 2 * time.Second
	ServiceRestartRequeueDuration     = 10 * time.Second
	DashboardUnhealthySecondThreshold = 60.0
	servicePortName                   = "dashboard"
)

// RayServiceReconciler reconciles a RayService object
type RayServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
	// Now Ray dashboard does not cache serve deployment config. To avoid updating the same config repeatedly, cache the Serve Deployment config in this map.
	ServeDeploymentConfigs cmap.ConcurrentMap
}

// NewRayServiceReconciler returns a new reconcile.Reconciler
func NewRayServiceReconciler(mgr manager.Manager) *RayServiceReconciler {
	return &RayServiceReconciler{
		Client:                 mgr.GetClient(),
		Scheme:                 mgr.GetScheme(),
		Log:                    ctrl.Log.WithName("controllers").WithName("RayService"),
		Recorder:               mgr.GetEventRecorderFor("rayservice-controller"),
		ServeDeploymentConfigs: cmap.New(),
	}
}

// +kubebuilder:rbac:groups=ray.io,resources=rayservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/finalizer,verbs=update
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

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RayService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *RayServiceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("rayservice", request.NamespacedName)
	rayServiceLog.Info("reconciling RayService", "service NamespacedName", request.NamespacedName)

	// Get serving cluster instance
	var err error
	var rayServiceInstance *rayv1alpha1.RayService

	if rayServiceInstance, err = r.getRayServiceInstance(ctx, request); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Reconcile RayCluster, check if we need to create a new RayCluster.
	var activeRayClusterInstance *rayv1alpha1.RayCluster
	var pendingRayClusterInstance *rayv1alpha1.RayCluster
	if activeRayClusterInstance, pendingRayClusterInstance, err = r.reconcileRayCluster(ctx, rayServiceInstance); err != nil {
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailToGetOrCreateRayCluster, err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Reconcile Serve Configs.
	r.cleanUpServeConfigMaps(rayServiceInstance)

	rayServiceLog.Info("Done reconcileRayCluster")

	// Check if we need to create pending RayCluster.
	if rayServiceInstance.Status.PendingRayClusterName != "" && pendingRayClusterInstance == nil {
		// Update RayService Status since reconcileRayCluster may mark RayCluster restart.
		if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
			rayServiceLog.Error(errStatus, "Fail to update status of RayService", "rayServiceInstance", rayServiceInstance)
			return ctrl.Result{}, err
		}
		rayServiceLog.Info("Done reconcileRayCluster update status")

		rayServiceLog.Info("Enter next loop to create new ray cluster.")
		return ctrl.Result{}, nil
	}

	rayClusterInstance := activeRayClusterInstance // RayCluster instance which needs reconcile.
	if pendingRayClusterInstance != nil {
		rayClusterInstance = pendingRayClusterInstance
	}

	rayServiceInstance.Status.RayClusterStatus = rayClusterInstance.Status

	var clientURL string
	if clientURL, err = r.fetchDashboardURL(ctx, rayClusterInstance); err != nil || clientURL == "" {
		if !r.updateAndCheckDashboardStatus(rayServiceInstance, false) {
			rayServiceLog.Info("Dashboard is unhealthy, restart the cluster.")
			r.markRestart(rayServiceInstance)
		}
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.WaitForDashboard, err)
		return ctrl.Result{}, err
	}

	rayDashboardClient := utils.GetRayDashboardClientFunc()
	rayDashboardClient.InitClient(clientURL)

	shouldUpdate := r.checkIfNeedSubmitServeDeployment(rayServiceInstance, rayClusterInstance, request)

	if shouldUpdate {
		if err = r.updateServeDeployment(rayServiceInstance, rayDashboardClient, rayClusterInstance.Name, request); err != nil {
			if !r.updateAndCheckDashboardStatus(rayServiceInstance, false) {
				rayServiceLog.Info("Dashboard is unhealthy, restart the cluster.")
				r.markRestart(rayServiceInstance)
			}
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailServeDeploy, err)
			return ctrl.Result{}, err
		}
	}

	var isHealthy bool
	if isHealthy, err = r.getAndCheckServeStatus(rayServiceInstance, rayDashboardClient); err != nil {
		if !r.updateAndCheckDashboardStatus(rayServiceInstance, false) {
			rayServiceLog.Info("Dashboard is unhealthy, restart the cluster.")
			r.markRestart(rayServiceInstance)
		}
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailGetServeDeploymentStatus, err)
		return ctrl.Result{}, err
	}

	r.updateAndCheckDashboardStatus(rayServiceInstance, true)

	rayServiceLog.Info("Check serve health", "isHealthy", isHealthy)

	if isHealthy {
		rayServiceInstance.Status.ServiceStatus = rayv1alpha1.Running
		if r.allServeDeploymentsHealthy(rayServiceInstance) {
			// Preparing RayCluster is ready.
			r.updateRayClusterInfo(rayServiceInstance, rayClusterInstance.Name)
		}
	} else {
		r.markRestart(rayServiceInstance)
		rayServiceInstance.Status.ServiceStatus = rayv1alpha1.Restarting
		if err := r.Status().Update(ctx, rayServiceInstance); err != nil {
			return ctrl.Result{}, err
		}

		rayServiceLog.V(1).Info("Mark cluster as unhealthy", "rayCluster", rayClusterInstance)
		// Wait a while for the cluster delete
		return ctrl.Result{RequeueAfter: ServiceRestartRequeueDuration}, nil
	}

	if rayClusterInstance != nil {
		if err := r.reconcileIngress(ctx, rayServiceInstance, rayClusterInstance); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailUpdateIngress, err)
			return ctrl.Result{}, err
		}
		if err := r.reconcileServices(ctx, rayServiceInstance, rayClusterInstance); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailUpdateService, err)
			return ctrl.Result{}, err
		}
	}

	if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
		rayServiceLog.Error(err, "Fail to update status of RayService", "rayServiceInstance", rayServiceInstance)
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: ServiceDefaultRequeueDuration}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1alpha1.RayService{}).
		Owns(&rayv1alpha1.RayCluster{}).
		Watches(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &rayv1alpha1.RayService{},
		}).
		Watches(&source.Kind{Type: &networkingv1.Ingress{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &rayv1alpha1.RayService{},
		}).
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
	return err
}

func (r *RayServiceReconciler) reconcileRayCluster(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService) (*rayv1alpha1.RayCluster, *rayv1alpha1.RayCluster, error) {
	var err error
	if err = r.cleanUpRayClusterInstance(ctx, rayServiceInstance); err != nil {
		return nil, nil, err
	}

	// Get active cluster and pending cluster instances.
	activeRayCluster, err := r.getRayClusterByNameSpaceName(ctx, client.ObjectKey{Name: rayServiceInstance.Status.ActiveRayClusterName, Namespace: rayServiceInstance.Namespace})

	if err != nil {
		return nil, nil, err
	}

	pendingRayCluster, err := r.getRayClusterByNameSpaceName(ctx, client.ObjectKey{Name: rayServiceInstance.Status.PendingRayClusterName, Namespace: rayServiceInstance.Namespace})

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

func (r *RayServiceReconciler) cleanUpRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService) error {
	rayClusterList := rayv1alpha1.RayClusterList{}
	filterLabels := client.MatchingLabels{common.RayServiceLabelKey: rayServiceInstance.Name}
	var err error
	if err = r.List(ctx, &rayClusterList, client.InNamespace(rayServiceInstance.Namespace), filterLabels); err != nil {
		r.Log.Error(err, "Fail to list RayCluster for "+rayServiceInstance.Name)
		return err
	}

	// Clean up RayCluster instances.
	for _, rayClusterInstance := range rayClusterList.Items {
		if rayClusterInstance.Name != rayServiceInstance.Status.ActiveRayClusterName && rayClusterInstance.Name != rayServiceInstance.Status.PendingRayClusterName {
			r.Log.V(1).Info("reconcileRayCluster", "delete ray cluster", rayClusterInstance.Name)
			if err := r.Delete(ctx, &rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
				r.Log.Error(err, "Fail to delete RayCluster "+rayClusterInstance.Name)
				return err
			}
		}
	}

	return nil
}

func (r *RayServiceReconciler) getRayClusterByNameSpaceName(ctx context.Context, clusterKey client.ObjectKey) (*rayv1alpha1.RayCluster, error) {
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

func (r *RayServiceReconciler) cleanUpServeConfigMaps(rayServiceInstance *rayv1alpha1.RayService) {
	activeConfigKey := r.generateConfigKey(rayServiceInstance, rayServiceInstance.Status.ActiveRayClusterName)
	pendingConfigKey := r.generateConfigKey(rayServiceInstance, rayServiceInstance.Status.PendingRayClusterName)
	configPrefix := r.generateConfigKeyPrefix(rayServiceInstance)

	// Clean up RayCluster serve deployment configs.
	for key := range r.ServeDeploymentConfigs.Items() {
		if key == activeConfigKey || key == pendingConfigKey {
			continue
		}
		if !strings.HasPrefix(key, configPrefix) {
			// Skip configs owned by other RayService Instance.
			continue
		}
		r.Log.V(1).Info("cleanUpServeConfigMaps", "activeConfigKey", activeConfigKey, "pendingConfigKey", pendingConfigKey, "remove key", key)
		r.ServeDeploymentConfigs.Remove(key)
	}
}

func (r *RayServiceReconciler) shouldPrepareNewRayCluster(rayServiceInstance *rayv1alpha1.RayService, activeRayCluster *rayv1alpha1.RayCluster) bool {
	shouldPrepareRayCluster := false

	if rayServiceInstance.Status.PendingRayClusterName == "" {
		// Prepare new RayCluster if:
		// 1. No active and pending cluster
		// 2. No pending cluster, and the active RayCluster has changed.
		if activeRayCluster == nil || !reflect.DeepEqual(activeRayCluster.Spec, rayServiceInstance.Spec.RayClusterSpec) {
			shouldPrepareRayCluster = true
		}
	}

	return shouldPrepareRayCluster
}

func (r *RayServiceReconciler) createRayClusterInstanceIfNeeded(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, pendingRayCluster *rayv1alpha1.RayCluster) (*rayv1alpha1.RayCluster, error) {
	if rayServiceInstance.Status.PendingRayClusterName == "" {
		// No exist pending RayCluster and no need to create one.
		return nil, nil
	}

	var err error
	// Create a new RayCluster if:
	// 1. No RayCluster pending.
	// 2. Config update for the pending cluster.
	if pendingRayCluster == nil || !reflect.DeepEqual(pendingRayCluster.Spec, rayServiceInstance.Spec.RayClusterSpec) {
		pendingRayCluster, err = r.createRayClusterInstance(ctx, rayServiceInstance, rayServiceInstance.Status.PendingRayClusterName)
		if err != nil {
			return nil, err
		}
	}

	return pendingRayCluster, nil
}

func (r *RayServiceReconciler) createRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstanceName string) (*rayv1alpha1.RayCluster, error) {
	r.Log.V(1).Info("createRayClusterInstance", "rayClusterInstanceName", rayClusterInstanceName)

	rayClusterKey := client.ObjectKey{
		Namespace: rayServiceInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1alpha1.RayCluster{}

	var err error
	// Loop until there is no pending RayCluster.
	for {
		err = r.Get(ctx, rayClusterKey, rayClusterInstance)

		// If RayCluster exists, it means the config is updated. Delete the previous RayCluster first.
		if err == nil {
			r.Log.V(1).Info("Ray cluster already exists, config changes. Need to recreate. Delete the pending one now.", "key", rayClusterKey.String())
			if delErr := r.Delete(ctx, rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); delErr != nil {
				if errors.IsNotFound(delErr) {
					break
				}
				return nil, delErr
			}
		} else if errors.IsNotFound(err) {
			break
		} else {
			r.Log.Error(err, "Get request rayCluster instance error!")
			return nil, err
		}
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
	r.Log.V(1).Info("created rayCluster for rayService run", "rayCluster", rayClusterInstance)

	return rayClusterInstance, nil
}

func (r *RayServiceReconciler) constructRayClusterForRayService(rayService *rayv1alpha1.RayService, rayClusterName string) (*rayv1alpha1.RayCluster, error) {
	rayClusterLabel := make(map[string]string)
	for k, v := range rayService.Labels {
		rayClusterLabel[k] = v
	}
	rayClusterLabel[common.RayServiceLabelKey] = rayService.Name

	rayCluster := &rayv1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      rayClusterLabel,
			Annotations: rayService.Annotations,
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

func (r *RayServiceReconciler) fetchDashboardURL(ctx context.Context, rayCluster *rayv1alpha1.RayCluster) (string, error) {
	headService := &corev1.Service{}
	headServiceName := utils.CheckName(utils.GenerateServiceName(rayCluster.Name))
	if err := r.Get(ctx, client.ObjectKey{Name: headServiceName, Namespace: rayCluster.Namespace}, headService); err != nil {
		return "", err
	}

	r.Log.V(1).Info("fetchDashboardURL ", "head service found", headService.Name)
	// TODO: compare diff and reconcile the object. For example. ServiceType might be changed or port might be modified
	servicePorts := headService.Spec.Ports

	dashboardPort := int32(-1)

	for _, servicePort := range servicePorts {
		if servicePort.Name == servicePortName {
			dashboardPort = servicePort.Port
			break
		}
	}

	if dashboardPort == int32(-1) {
		return "", fmtErrors.Errorf("dashboard port not found")
	}

	dashboardURL := fmt.Sprintf("%s.%s.svc.cluster.local:%v",
		headService.Name,
		headService.Namespace,
		dashboardPort)

	return dashboardURL, nil
}

func (r *RayServiceReconciler) checkIfNeedSubmitServeDeployment(rayServiceInstance *rayv1alpha1.RayService, rayClusterInstance *rayv1alpha1.RayCluster, request ctrl.Request) bool {
	existConfigObj, exist := r.ServeDeploymentConfigs.Get(r.generateConfigKey(rayServiceInstance, rayClusterInstance.Name))

	if !exist {
		r.Log.V(1).Info("shouldUpdate value, config does not exist")
		return true
	}

	existConfig, ok := existConfigObj.(rayv1alpha1.RayServiceSpec)

	shouldUpdate := false

	if !ok || !reflect.DeepEqual(existConfig, rayServiceInstance.Spec) || len(rayServiceInstance.Status.ServeStatuses) != len(existConfig.ServeConfigSpecs) {
		shouldUpdate = true
	}

	r.Log.V(1).Info("shouldUpdate value", "shouldUpdate", shouldUpdate)

	return shouldUpdate
}

func (r *RayServiceReconciler) updateServeDeployment(rayServiceInstance *rayv1alpha1.RayService, rayDashboardClient utils.RayDashboardClientInterface, clusterName string, request ctrl.Request) error {
	r.Log.V(1).Info("updateServeDeployment")
	if err := rayDashboardClient.UpdateDeployments(rayServiceInstance.Spec.ServeConfigSpecs); err != nil {
		r.Log.Error(err, "fail to update deployment")
		return err
	}

	r.ServeDeploymentConfigs.Set(r.generateConfigKey(rayServiceInstance, clusterName), rayServiceInstance.Spec)
	return nil
}

func (r *RayServiceReconciler) getAndCheckServeStatus(rayServiceInstance *rayv1alpha1.RayService, dashboardClient utils.RayDashboardClientInterface) (bool, error) {
	var serveStatuses *rayv1alpha1.ServeDeploymentStatuses
	var err error
	if serveStatuses, err = dashboardClient.GetDeploymentsStatus(); err != nil {
		r.Log.Error(err, "fail to get deployment status")
		return false, err
	}

	statusMap := make(map[string]rayv1alpha1.ServeDeploymentStatus)

	for _, status := range rayServiceInstance.Status.ServeStatuses {
		statusMap[status.Name] = status
	}

	isHealthy := true
	for i := 0; i < len(serveStatuses.Statuses); i++ {
		timeNow := metav1.Now()
		serveStatuses.Statuses[i].LastUpdateTime = &timeNow
		serveStatuses.Statuses[i].HealthLastUpdateTime = &timeNow
		if serveStatuses.Statuses[i].Status != "HEALTHY" {
			prevStatus, exist := statusMap[serveStatuses.Statuses[i].Name]
			if exist {
				if prevStatus.Status != "HEALTHY" {
					serveStatuses.Statuses[i].HealthLastUpdateTime = prevStatus.HealthLastUpdateTime

					if time.Since(prevStatus.HealthLastUpdateTime.Time).Seconds() > ServeDeploymentUnhealthySecondThreshold {
						isHealthy = false
					}
				}
			}
		}
	}

	rayServiceInstance.Status.ServeStatuses = serveStatuses.Statuses

	r.Log.V(1).Info("getAndCheckServeStatus ", "statusMap", statusMap, "serveStatuses", serveStatuses)

	return isHealthy, nil
}

func (r *RayServiceReconciler) allServeDeploymentsHealthy(rayServiceInstance *rayv1alpha1.RayService) bool {
	// Check if the serve deployment number is correct.
	r.Log.V(1).Info("allServeDeploymentsHealthy", "rayServiceInstance.Status.ServeStatuses", rayServiceInstance.Status.ServeStatuses)
	if len(rayServiceInstance.Status.ServeStatuses) != len(rayServiceInstance.Spec.ServeConfigSpecs) {
		return false
	}

	// Check if all the service deployments are healthy.
	for _, status := range rayServiceInstance.Status.ServeStatuses {
		if status.Status != "HEALTHY" {
			return false
		}
	}

	return true
}

func (r *RayServiceReconciler) generateConfigKey(rayServiceInstance *rayv1alpha1.RayService, clusterName string) string {
	return r.generateConfigKeyPrefix(rayServiceInstance) + clusterName
}

func (r *RayServiceReconciler) generateConfigKeyPrefix(rayServiceInstance *rayv1alpha1.RayService) string {
	return rayServiceInstance.Namespace + "/" + rayServiceInstance.Name + "/"
}

// Return true if healthy, otherwise false.
func (r *RayServiceReconciler) updateAndCheckDashboardStatus(rayServiceInstance *rayv1alpha1.RayService, isHealthy bool) bool {
	timeNow := metav1.Now()
	rayServiceInstance.Status.DashboardStatus.LastUpdateTime = &timeNow
	rayServiceInstance.Status.DashboardStatus.IsHealthy = isHealthy
	if rayServiceInstance.Status.DashboardStatus.HealthLastUpdateTime.IsZero() || isHealthy {
		rayServiceInstance.Status.DashboardStatus.HealthLastUpdateTime = &timeNow
	}

	return time.Since(rayServiceInstance.Status.DashboardStatus.HealthLastUpdateTime.Time).Seconds() <= DashboardUnhealthySecondThreshold
}

func (r *RayServiceReconciler) markRestart(rayServiceInstance *rayv1alpha1.RayService) {
	// Generate RayCluster name for pending cluster.
	r.Log.V(1).Info("Current cluster is unhealthy, prepare to restart.", "Status", rayServiceInstance.Status)
	rayServiceInstance.Status = rayv1alpha1.RayServiceStatus{
		ActiveRayClusterName:  rayServiceInstance.Status.ActiveRayClusterName,
		PendingRayClusterName: utils.GenerateRayClusterName(rayServiceInstance.Name),
		ServiceStatus:         rayv1alpha1.Restarting,
	}
}

func (r *RayServiceReconciler) updateRayClusterInfo(rayServiceInstance *rayv1alpha1.RayService, healthyClusterName string) {
	r.Log.V(1).Info("updateRayClusterInfo", "ActiveRayClusterName", rayServiceInstance.Status.ActiveRayClusterName, "healthyClusterName", healthyClusterName)
	if rayServiceInstance.Status.ActiveRayClusterName != healthyClusterName {
		rayServiceInstance.Status.ActiveRayClusterName = healthyClusterName
		rayServiceInstance.Status.PendingRayClusterName = ""
	}
}

func (r *RayServiceReconciler) reconcileIngress(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstance *rayv1alpha1.RayCluster) error {
	if rayClusterInstance.Spec.HeadGroupSpec.EnableIngress == nil || !*rayClusterInstance.Spec.HeadGroupSpec.EnableIngress {
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
				log.Info("Ingress already exists,no need to create")
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

func (r *RayServiceReconciler) reconcileServices(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstance *rayv1alpha1.RayCluster) error {
	// Creat Service Struct.
	rayHeadSvc, err := common.BuildServiceForRayService(*rayServiceInstance, *rayClusterInstance)
	if err != nil {
		return err
	}
	rayHeadSvc.Name = utils.CheckName(rayHeadSvc.Name)

	// Get Service instance.
	headService := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Name: rayHeadSvc.Name, Namespace: rayServiceInstance.Namespace}, headService)

	if err == nil {
		// Update Service
		headService.Spec = rayHeadSvc.Spec
		r.Log.V(1).Info("reconcileServices update service")
		if updateErr := r.Update(ctx, headService); updateErr != nil {
			r.Log.Error(updateErr, "rayHeadSvc Update error!", "rayHeadSvc.Error", updateErr)
			return updateErr
		}
	} else if errors.IsNotFound(err) {
		// Create Service
		r.Log.V(1).Info("reconcileServices create service")
		if err := ctrl.SetControllerReference(rayServiceInstance, rayHeadSvc, r.Scheme); err != nil {
			return err
		}
		if createErr := r.Create(ctx, rayHeadSvc); createErr != nil {
			if errors.IsAlreadyExists(createErr) {
				log.Info("rayHeadSvc already exists,no need to create")
				return nil
			}
			r.Log.Error(createErr, "rayHeadSvc create error!", "rayHeadSvc.Error", createErr)
			return createErr
		}
	} else {
		r.Log.Error(err, "rayHeadSvc get error!")
		return err
	}

	return nil
}
