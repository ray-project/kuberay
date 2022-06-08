package ray

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
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
	RayServiceDefaultRequeueDuration          = 2 * time.Second
	RayServiceRestartRequeueDuration          = 10 * time.Second
	RayServeDeploymentUnhealthSecondThreshold = 60.0
	rayClusterSuffix                          = "-raycluster-"
	servicePortName                           = "dashboard"
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
	log.Info("reconciling RayService", "service NamespacedName", request.NamespacedName)

	// Get serving cluster instance
	var err error
	var rayServiceInstance *rayv1alpha1.RayService

	if rayServiceInstance, err = r.getRayServiceInstance(ctx, request); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var rayClusterInstance *rayv1alpha1.RayCluster
	if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayServiceInstance); err != nil {
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailToGetOrCreateRayCluster, err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rayServiceInstance.Status.RayClusterStatus = rayClusterInstance.Status

	var clientURL string
	if clientURL, err = r.fetchDashboardURL(ctx, rayClusterInstance); err != nil || clientURL == "" {
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.WaitForDashboard, err)
		return ctrl.Result{}, err
	}

	rayDashboardClient := utils.RayDashboardClient{}
	rayDashboardClient.InitClient(clientURL)

	shouldUpdate := r.checkIfNeedSubmitServeDeployment(rayServiceInstance, request)

	if shouldUpdate {
		if err = r.updateServeDeployment(rayServiceInstance, rayDashboardClient, request); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailServeDeploy, err)
			return ctrl.Result{}, err
		}
	}

	var isHealthy bool
	if isHealthy, err = r.getAndCheckServeStatus(rayServiceInstance, rayDashboardClient); err != nil {
		err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailGetServeDeploymentStatus, err)
		return ctrl.Result{}, err
	}

	log.Info("Check serve health", "isHealthy", isHealthy)

	if isHealthy {
		rayServiceInstance.Status.ServiceStatus = rayv1alpha1.Running
		if err := r.Status().Update(ctx, rayServiceInstance); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		rayServiceInstance.Status.RayClusterName = ""
		rayServiceInstance.Status.ServiceStatus = rayv1alpha1.Restarting
		if err := r.Status().Update(ctx, rayServiceInstance); err != nil {
			return ctrl.Result{}, err
		}

		r.ServeDeploymentConfigs.Remove(request.NamespacedName.String())

		// restart raycluster
		if err := r.Delete(ctx, rayClusterInstance); err != nil {
			err = r.updateState(ctx, rayServiceInstance, rayv1alpha1.FailDeleteRayCluster, err)
			return ctrl.Result{}, err
		}

		log.V(1).Info("Deleted rayCluster for rayService run", "rayCluster", rayClusterInstance)
		// Wait a while for the cluster delete
		return ctrl.Result{RequeueAfter: RayServiceRestartRequeueDuration}, nil
	}

	return ctrl.Result{RequeueAfter: RayServiceDefaultRequeueDuration}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1alpha1.RayService{}).
		Complete(r)
}

func (r *RayServiceReconciler) getRayServiceInstance(ctx context.Context, request ctrl.Request) (*rayv1alpha1.RayService, error) {
	rayServiceInstance := &rayv1alpha1.RayService{}
	if err := r.Get(ctx, request.NamespacedName, rayServiceInstance); err != nil {
		if errors.IsNotFound(err) {
			rayServiceLog.Info("Read request instance not found error!")
		} else {
			rayServiceLog.Error(err, "Read request instance error!")
		}
		// Error reading the object - requeue the request.
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

func (r *RayServiceReconciler) getOrCreateRayClusterInstance(ctx context.Context, rayServiceInstance *rayv1alpha1.RayService) (*rayv1alpha1.RayCluster, error) {
	// Update ray cluster
	var rayClusterInstanceName string
	if rayServiceInstance.Status.RayClusterName != "" {
		rayClusterInstanceName = rayServiceInstance.Status.RayClusterName
	} else {
		rayClusterInstanceName = rayServiceInstance.Name + rayClusterSuffix + uuid.New().String()
	}

	rayClusterNamespacedName := types.NamespacedName{
		Namespace: rayServiceInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1alpha1.RayCluster{}
	err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance)

	if err == nil {
		rayClusterInstance.Spec = rayServiceInstance.Spec.RayClusterSpec

		r.Log.Info("Update ray cluster spec")
		// TODO: Check which config fields can be handled by RayCluster controller. For other fields, need to start a new RayCluster.
		if err := r.Update(ctx, rayClusterInstance); err != nil {
			r.Log.Error(err, "Fail to update ray cluster instance!")
			// Error updating the RayCluster object.
			return nil, client.IgnoreNotFound(err)
		}
	} else if errors.IsNotFound(err) {
		r.Log.Info("Not found rayCluster, creating rayCluster!")
		rayClusterInstance, err = r.constructRayClusterForRayService(rayServiceInstance)
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
		// Update RayCluster name in RayService state.
		rayServiceInstance.Status.RayClusterName = rayClusterInstanceName
		r.Log.V(1).Info("created rayCluster for rayService run", "rayCluster", rayClusterInstance)
	} else {
		r.Log.Error(err, "Get request rayCluster instance error!")
		// Error reading the RayCluster object - requeue the request.
		return nil, err
	}

	return rayClusterInstance, nil
}

func (r *RayServiceReconciler) constructRayClusterForRayService(rayService *rayv1alpha1.RayService) (*rayv1alpha1.RayCluster, error) {
	name := fmt.Sprintf("%s%s", rayService.Name, rayClusterSuffix)

	rayCluster := &rayv1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      rayService.Labels,
			Annotations: rayService.Annotations,
			Name:        name,
			Namespace:   rayService.Namespace,
		},
		Spec: *rayService.Spec.RayClusterSpec.DeepCopy(),
	}

	// Set the ownership in order to do the garbage collection by k8s.
	if err := ctrl.SetControllerReference(rayService, rayCluster, r.Scheme); err != nil {
		return nil, err
	}

	return rayCluster, nil
}

func (r *RayServiceReconciler) fetchDashboardURL(ctx context.Context, rayCluster *rayv1alpha1.RayCluster) (string, error) {
	var headService *corev1.Service
	if err := r.Get(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, headService); err != nil {
		return "", err
	}

	r.Log.Info("reconcileServices ", "head service found", headService.Name)
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

func (r *RayServiceReconciler) checkIfNeedSubmitServeDeployment(rayServiceInstance *rayv1alpha1.RayService, request ctrl.Request) bool {
	existConfigObj, exist := r.ServeDeploymentConfigs.Get(request.NamespacedName.String())

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

func (r *RayServiceReconciler) updateServeDeployment(rayServiceInstance *rayv1alpha1.RayService, rayDashboardClient utils.RayDashboardClient, request ctrl.Request) error {
	r.Log.Info("shouldUpdate")
	if err := rayDashboardClient.UpdateDeployments(rayServiceInstance.Spec.ServeConfigSpecs); err != nil {
		r.Log.Error(err, "fail to update deployment")
		return err
	}

	r.ServeDeploymentConfigs.Set(request.NamespacedName.String(), rayServiceInstance.Spec)
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

					if time.Since(prevStatus.HealthLastUpdateTime.Time).Seconds() > RayServeDeploymentUnhealthSecondThreshold {
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
