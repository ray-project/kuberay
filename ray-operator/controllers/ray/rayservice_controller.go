package ray

import (
	"context"
	"fmt"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	networkingv1 "k8s.io/api/networking/v1"

	cmap "github.com/orcaman/concurrent-map"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

var (
	rayServiceLog = logf.Log.WithName("rayservice-controller")
)

const (
	RayServiceDefaultRequeueDuration           = 2 * time.Second
	RayServiceRestartRequeueDuration           = 10 * time.Second
	RayServeDeploymentUnhealthySecondThreshold = 300.0
	RayDashboardUnhealthySecondThreshold       = 600.0
	servicePortName                            = "dashboard"
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

	var runningRayClusterInstance *rayv1alpha1.RayCluster
	var preparingRayClusterInstance *rayv1alpha1.RayCluster
	if runningRayClusterInstance, preparingRayClusterInstance, err = r.reconcileRayCluster(ctx, rayServiceInstance); err != nil {
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailToGetOrCreateRayCluster, err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rayServiceLog.Info("Done reconcileRayCluster")
	if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
		rayServiceLog.Error(errStatus, "Fail to update status of RayService", "rayServiceInstance", rayServiceInstance)
		return ctrl.Result{}, err
	}

	if rayServiceInstance.Status.PreparingRayClusterName != "" && preparingRayClusterInstance == nil {
		r.Log.Info("Enter next loop to create new ray cluster.")
		return ctrl.Result{}, nil
	}

	rayServiceLog.Info("Done reconcileRayCluster update status")

	rayClusterInstance := runningRayClusterInstance
	servingRayClusterInstance := runningRayClusterInstance
	if preparingRayClusterInstance != nil {
		rayClusterInstance = preparingRayClusterInstance
	}

	rayServiceLog.Info("Updated RayCluster")

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

	rayDashboardClient := utils.RayDashboardClient{}
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
			r.updateRayClusterHealthInfo(rayServiceInstance, rayClusterInstance.Name)
		}
		// If the rayClusterInstance is healthy, update servingRayClusterInstance.
		servingRayClusterInstance = rayClusterInstance
		if err := r.Status().Update(ctx, rayServiceInstance); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		r.markRestart(rayServiceInstance)
		rayServiceInstance.Status.ServiceStatus = rayv1alpha1.Restarting
		if err := r.Status().Update(ctx, rayServiceInstance); err != nil {
			return ctrl.Result{}, err
		}

		rayServiceLog.V(1).Info("Mark cluster as unhealthy", "rayCluster", rayClusterInstance)
		// Wait a while for the cluster delete
		return ctrl.Result{RequeueAfter: RayServiceRestartRequeueDuration}, nil
	}

	if rayClusterInstance != nil {
		if err := r.reconcileIngress(ctx, rayServiceInstance, servingRayClusterInstance); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailUpdateIngress, err)
			return ctrl.Result{}, err
		}
		if err := r.reconcileServices(ctx, rayServiceInstance, servingRayClusterInstance); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailUpdateService, err)
			return ctrl.Result{}, err
		}
	}

	if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
		rayServiceLog.Error(err, "Fail to update status of RayService", "rayServiceInstance", rayServiceInstance)
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: RayServiceDefaultRequeueDuration}, nil
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
		// Error reading the object - requeue the request.
		return nil, err
	}
	return rayServiceInstance, nil
}

func (r *RayServiceReconciler) updateState(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, status rayv1alpha1.ServiceStatus, err error) error {
	rayServiceInstance.Status.ServiceStatus = status
	if errStatus := r.Status().Update(ctx, rayServiceInstance); errStatus != nil {
		return fmtErrors.Errorf("combined error: \n %v \n %v", err, errStatus)
	}
	return err
}

func (r *RayServiceReconciler) reconcileRayCluster(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService) (*rayv1alpha1.RayCluster, *rayv1alpha1.RayCluster, error) {
	rayClusterList := rayv1alpha1.RayClusterList{}
	filterLabels := client.MatchingLabels{common.RayServiceLabelKey: rayServiceInstance.Name}
	var err error
	if err = r.List(ctx, &rayClusterList, client.InNamespace(rayServiceInstance.Namespace), filterLabels); err != nil {
		return nil, nil, err
	}

	// Clean up RayCluster instances.
	for _, rayClusterInstance := range rayClusterList.Items {
		if rayClusterInstance.Name != rayServiceInstance.Status.RayClusterName && rayClusterInstance.Name != rayServiceInstance.Status.PreparingRayClusterName {
			r.Log.Info("reconcileRayCluster", "delete ray cluster", rayClusterInstance)
			if err := r.Delete(ctx, &rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
				return nil, nil, err
			}
		}
	}

	// Get running cluster and preparing cluster instances.
	runningRayCluster := &rayv1alpha1.RayCluster{}
	if rayServiceInstance.Status.RayClusterName != "" {
		if err = r.Get(ctx, client.ObjectKey{Name: rayServiceInstance.Status.RayClusterName, Namespace: rayServiceInstance.Namespace}, runningRayCluster); err != nil {
			runningRayCluster = nil
		}
	} else {
		runningRayCluster = nil
	}

	preparingRayCluster := &rayv1alpha1.RayCluster{}
	if rayServiceInstance.Status.PreparingRayClusterName != "" {
		if err = r.Get(ctx, client.ObjectKey{Name: rayServiceInstance.Status.PreparingRayClusterName, Namespace: rayServiceInstance.Namespace}, preparingRayCluster); err != nil {
			preparingRayCluster = nil
		}
	} else {
		preparingRayCluster = nil
	}

	runningConfigKey := r.generateConfigKey(rayServiceInstance, rayServiceInstance.Status.RayClusterName)
	preparingConfigKey := r.generateConfigKey(rayServiceInstance, rayServiceInstance.Status.PreparingRayClusterName)
	// Clean up RayCluster serve deployment configs.
	for key := range r.ServeDeploymentConfigs.Items() {
		if key == runningConfigKey || key == preparingConfigKey {
			continue
		}
		r.Log.Info("reconcileRayCluster removing config", "runningConfigKey", runningConfigKey, "preparingConfigKey", preparingConfigKey, "removekey", key)
		r.ServeDeploymentConfigs.Remove(key)
	}

	shouldGeneratePreparingRayClusterName := false

	if rayServiceInstance.Status.PreparingRayClusterName == "" {
		if runningRayCluster == nil || !reflect.DeepEqual(runningRayCluster.Spec, rayServiceInstance.Spec.RayClusterSpec) {
			shouldGeneratePreparingRayClusterName = true
		}
	}

	if shouldGeneratePreparingRayClusterName {
		r.markRestart(rayServiceInstance)
		return runningRayCluster, nil, nil
	}

	if rayServiceInstance.Status.PreparingRayClusterName != "" {
		shouldCreatePreparingRayCluster := false
		// Check whether to create a new RayCluster.
		if preparingRayCluster == nil || !reflect.DeepEqual(preparingRayCluster.Spec, rayServiceInstance.Spec.RayClusterSpec) {
			shouldCreatePreparingRayCluster = true
		}

		if shouldCreatePreparingRayCluster {
			preparingRayCluster, err = r.createRayClusterInstance(ctx, rayServiceInstance, rayServiceInstance.Status.PreparingRayClusterName)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	return runningRayCluster, preparingRayCluster, nil
}

func (r *RayServiceReconciler) createRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService, rayClusterInstanceName string) (*rayv1alpha1.RayCluster, error) {
	r.Log.Info("createRayClusterInstance", "rayClusterInstanceName", rayClusterInstanceName)

	rayClusterNamespacedName := types.NamespacedName{
		Namespace: rayServiceInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1alpha1.RayCluster{}
	err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance)

	if err == nil {
		// Should not reach here unless the rand generation conflict.
		r.Log.Info("Ray cluster already exists, config changes, need to restart, delete the preparing one now.")
		if delErr := r.Delete(ctx, rayClusterInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); delErr != nil {
			return nil, delErr
		}
	}

	if errors.IsNotFound(err) {
		r.Log.Info("Not found rayCluster, creating rayCluster!")
		rayClusterInstance, err = r.constructRayClusterForRayService(rayServiceInstance, rayClusterInstanceName)
		if err != nil {
			r.Log.Error(err, "unable to construct rayCluster from spec")
			// Error construct the RayCluster object - requeue the request.
			return nil, err
		}
		if err := r.Create(ctx, rayClusterInstance); err != nil {
			r.Log.Error(err, "unable to create rayCluster for rayService", "rayCluster", rayClusterInstance)
			// Error creating the RayCluster object - requeue the request.
			return nil, err
		}
		r.Log.V(1).Info("created rayCluster for rayService run", "rayCluster", rayClusterInstance)
	} else {
		r.Log.Error(err, "Get request rayCluster instance error!")
		// Error reading the RayCluster object - requeue the request.
		return nil, err
	}

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

	r.Log.Info("fetchDashboardURL ", "head service found", headService.Name)
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
		r.Log.Info("shouldUpdate value, config does not exist")
		return true
	}

	existConfig, ok := existConfigObj.(rayv1alpha1.RayServiceSpec)

	shouldUpdate := false

	if !ok || !reflect.DeepEqual(existConfig, rayServiceInstance.Spec) || len(rayServiceInstance.Status.ServeStatuses) != len(existConfig.ServeConfigSpecs) {
		shouldUpdate = true
	}

	r.Log.Info("shouldUpdate value", "shouldUpdate", shouldUpdate)

	return shouldUpdate
}

func (r *RayServiceReconciler) updateServeDeployment(rayServiceInstance *rayv1alpha1.RayService, rayDashboardClient utils.RayDashboardClient, clusterName string, request ctrl.Request) error {
	r.Log.Info("updateServeDeployment")
	if err := rayDashboardClient.UpdateDeployments(rayServiceInstance.Spec.ServeConfigSpecs); err != nil {
		r.Log.Error(err, "fail to update deployment")
		return err
	}

	r.ServeDeploymentConfigs.Set(r.generateConfigKey(rayServiceInstance, clusterName), rayServiceInstance.Spec)
	return nil
}

func (r *RayServiceReconciler) getAndCheckServeStatus(rayServiceInstance *rayv1alpha1.RayService, dashboardClient utils.RayDashboardClient) (bool, error) {
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
		serveStatuses.Statuses[i].LastUpdateTime = metav1.Now()
		serveStatuses.Statuses[i].HealthLastUpdateTime = metav1.Now()
		if serveStatuses.Statuses[i].Status != "HEALTHY" {
			prevStatus, exist := statusMap[serveStatuses.Statuses[i].Name]
			if exist {
				if prevStatus.Status != "HEALTHY" {
					serveStatuses.Statuses[i].HealthLastUpdateTime = prevStatus.HealthLastUpdateTime

					if time.Since(prevStatus.HealthLastUpdateTime.Time).Seconds() > RayServeDeploymentUnhealthySecondThreshold {
						isHealthy = false
					}
				}
			}
		}
	}

	rayServiceInstance.Status.ServeStatuses = serveStatuses.Statuses

	r.Log.Info("getAndCheckServeStatus ", "statusMap", statusMap, "serveStatuses", serveStatuses)

	return isHealthy, nil
}

func (r *RayServiceReconciler) allServeDeploymentsHealthy(rayServiceInstance *rayv1alpha1.RayService) bool {
	// Check if the serve deployment number is correct.
	r.Log.Info("allServeDeploymentsHealthy", "rayServiceInstance.Status.ServeStatuses", rayServiceInstance.Status.ServeStatuses)
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

func (r *RayServiceReconciler) generateConfigKey(rayServiceInstance *rayv1alpha1.RayService, clusterName string) string {
	return rayServiceInstance.Namespace + "/" + rayServiceInstance.Name + "/" + clusterName
}

// Return true if healthy, otherwise false.
func (r *RayServiceReconciler) updateAndCheckDashboardStatus(rayServiceInstance *rayv1alpha1.RayService, isHealthy bool) bool {
	rayServiceInstance.Status.DashBoardStatus.LastUpdateTime = metav1.Now()
	rayServiceInstance.Status.DashBoardStatus.IsHealthy = isHealthy
	if rayServiceInstance.Status.DashBoardStatus.HealthLastUpdateTime.IsZero() || isHealthy {
		rayServiceInstance.Status.DashBoardStatus.HealthLastUpdateTime = metav1.Now()
	}

	return time.Since(rayServiceInstance.Status.DashBoardStatus.HealthLastUpdateTime.Time).Seconds() <= RayDashboardUnhealthySecondThreshold
}

func (r *RayServiceReconciler) markRestart(rayServiceInstance *rayv1alpha1.RayService) {
	r.Log.Info("Current cluster is unhealthy, prepare to restart.", "pre rayServiceInstance.Status", rayServiceInstance.Status)
	rayServiceInstance.Status = rayv1alpha1.RayServiceStatus{
		RayClusterName:          rayServiceInstance.Status.RayClusterName,
		PreparingRayClusterName: utils.GenerateRayClusterName(rayServiceInstance.Name),
		ServiceStatus:           rayv1alpha1.Restarting,
	}
	r.Log.Info("Current cluster is unhealthy, prepare to restart.", "rayServiceInstance.Status", rayServiceInstance.Status)
}

func (r *RayServiceReconciler) updateRayClusterHealthInfo(rayServiceInstance *rayv1alpha1.RayService, healthyClusterName string) {
	r.Log.Info("updateRayClusterHealthInfo", "rayServiceInstance.Status.RayClusterName", rayServiceInstance.Status.RayClusterName, "healthyClusterName", healthyClusterName)
	if rayServiceInstance.Status.RayClusterName != healthyClusterName {
		rayServiceInstance.Status.RayClusterName = healthyClusterName
		rayServiceInstance.Status.PreparingRayClusterName = ""
	}
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
		r.Log.Info("reconcileServices update service")
		if updateErr := r.Update(ctx, headService); updateErr != nil {
			r.Log.Error(updateErr, "rayHeadSvc Update error!", "rayHeadSvc.Error", updateErr)
			return updateErr
		}
	} else if errors.IsNotFound(err) {
		// Create Service
		r.Log.Info("reconcileServices create service")
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
