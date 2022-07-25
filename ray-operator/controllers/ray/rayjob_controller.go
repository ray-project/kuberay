package ray

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

const (
	RayJobDefaultRequeueDuration = 3 * time.Second
)

// RayJobReconciler reconciles a RayJob object
type RayJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// NewRayJobReconciler returns a new reconcile.Reconciler
func NewRayJobReconciler(mgr manager.Manager) *RayJobReconciler {
	return &RayJobReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("RayJob"),
	}
}

// +kubebuilder:rbac:groups=ray.io,resources=rayjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayjobs/finalizer,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;delete;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;delete

// Reconcile reads that state of a RayJob object and makes changes based on it
// and what is in the RayJob.Spec
// Automatically generate RBAC rules to allow the Controller to read and write workloads
// Reconcile used to bridge the desired state with the current state
func (r *RayJobReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("rayjob", request.NamespacedName)
	log.Info("reconciling RayJob", "NamespacedName", request.NamespacedName)

	// Get RayJob instance
	var err error
	var rayJobInstance *rayv1alpha1.RayJob

	if rayJobInstance, err = r.getRayJobInstance(ctx, request); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Set rayClusterName and rayJobId first, to avoid duplicate submission
	err = r.setRayJobIdAndRayClusterNameIfNeed(ctx, rayJobInstance)
	if err != nil {
		r.Log.Error(err, "failed to set jobId or raycluster name", "rayjob", request.NamespacedName)
		return ctrl.Result{}, err
	}
	var rayClusterInstance *rayv1alpha1.RayCluster
	if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayJobInstance); err != nil {
		err = r.updateState(ctx, rayJobInstance, rayv1alpha1.JobDeploymentStatusFailedToGetOrCreateRayCluster, err)
		return ctrl.Result{}, err
	}

	rayJobInstance.Status.RayClusterStatus = rayClusterInstance.Status

	clientURL := rayJobInstance.Status.DashboardURL
	if clientURL == "" {
		// TODO: dashboard service may be changed. Check it instead of using the same URL always
		if clientURL, err = utils.FetchDashboardURL(ctx, &r.Log, r.Client, rayClusterInstance); err != nil || clientURL == "" {
			if clientURL == "" {
				err = fmt.Errorf("empty dashboardURL")
			}
			err = r.updateState(ctx, rayJobInstance, rayv1alpha1.JobDeploymentStatusWaitForDashboard, err)
			return ctrl.Result{}, err
		}
		rayJobInstance.Status.DashboardURL = clientURL
	}

	rayDashboardClient := utils.GetRayDashboardClientFunc()
	rayDashboardClient.InitClient(clientURL)

	// Check the current status of ray cluster before submitting.
	if rayClusterInstance.Status.State != rayv1alpha1.Ready {
		r.Log.Info("waiting for the cluster to be ready", "rayCluster", rayClusterInstance.Name)
		err = r.updateState(ctx, rayJobInstance, rayv1alpha1.JobDeploymentStatusInitializing, nil)
		return ctrl.Result{}, err
	}

	// Check the current status of ray jobs before submitting.
	jobInfo, err := rayDashboardClient.GetJobInfo(rayJobInstance.Status.JobId)
	if err != nil {
		err = r.updateState(ctx, rayJobInstance, rayv1alpha1.JobDeploymentStatusFailedToGetJobStatus, err)
		return ctrl.Result{}, err
	}

	if jobInfo == nil {
		// Submit the job if no id set
		jobId, err := rayDashboardClient.SubmitJob(rayJobInstance, &r.Log)
		if err != nil {
			r.Log.Error(err, "failed to submit job")
			err = r.updateState(ctx, rayJobInstance, rayv1alpha1.JobDeploymentStatusFailedJobDeploy, err)
			return ctrl.Result{}, err
		}
		log.Info("Job successfully submitted", "jobId", jobId)
		rayJobInstance.Status.JobStatus = rayv1alpha1.JobStatusPending
		err = r.updateState(ctx, rayJobInstance, rayv1alpha1.JobDeploymentStatusRunning, nil)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	}

	if jobInfo.JobStatus != rayJobInstance.Status.JobStatus {
		// TODO: if the job fails, we should retry
		r.Log.Info(fmt.Sprintf("Update ray job %s status from %s to %s", rayJobInstance.Status.JobId, rayJobInstance.Status.JobStatus, jobInfo.JobStatus))
		rayJobInstance.Status.JobStatus = jobInfo.JobStatus
		err = r.updateState(ctx, rayJobInstance, rayv1alpha1.JobDeploymentStatusRunning, nil)
		return ctrl.Result{}, err
	}

	if rayJobInstance.Status.JobDeploymentStatus != rayv1alpha1.JobDeploymentStatusRunning {
		err = r.updateState(ctx, rayJobInstance, rayv1alpha1.JobDeploymentStatusRunning, nil)
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1alpha1.RayJob{}).
		Owns(&rayv1alpha1.RayCluster{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

func (r *RayJobReconciler) getRayJobInstance(ctx context.Context, request ctrl.Request) (*rayv1alpha1.RayJob, error) {
	rayJobInstance := &rayv1alpha1.RayJob{}
	if err := r.Get(ctx, request.NamespacedName, rayJobInstance); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Read request instance not found error!")
		} else {
			r.Log.Error(err, "Read request instance error!")
		}
		// Error reading the object - requeue the request.
		return nil, err
	}
	return rayJobInstance, nil
}

func (r *RayJobReconciler) setRayJobIdAndRayClusterNameIfNeed(ctx context.Context, rayJob *rayv1alpha1.RayJob) error {
	shouldUpdateStatus := false
	if rayJob.Status.JobId == "" {
		shouldUpdateStatus = true
		if rayJob.Spec.JobId != "" {
			rayJob.Status.JobId = rayJob.Spec.JobId
		} else {
			rayJob.Status.JobId = utils.GenerateRayJobId(rayJob.Name)
		}
	}
	if rayJob.Status.RayClusterName == "" {
		shouldUpdateStatus = true
		rayJob.Status.RayClusterName = utils.GenerateRayClusterName(rayJob.Name)
	}

	if shouldUpdateStatus {
		return r.updateState(ctx, rayJob, rayv1alpha1.JobDeploymentStatusInitializing, nil)
	}
	return nil
}

func (r *RayJobReconciler) updateState(ctx context.Context, rayJob *rayv1alpha1.RayJob, status rayv1alpha1.JobDeploymentStatus, err error) error {
	rayJob.Status.JobDeploymentStatus = status
	if errStatus := r.Status().Update(ctx, rayJob); errStatus != nil {
		return fmtErrors.Errorf("combined error: %v %v", err, errStatus)
	}
	return err
}

// TODO: select existing rayclusters by ClusterSelector
func (r *RayJobReconciler) getOrCreateRayClusterInstance(ctx context.Context, rayJobInstance *rayv1alpha1.RayJob) (*rayv1alpha1.RayCluster, error) {
	// Update ray cluster
	rayClusterInstanceName := rayJobInstance.Status.RayClusterName

	r.Log.Info("getOrCreateRayClusterInstance", "rayClusterInstanceName", rayClusterInstanceName)

	rayClusterNamespacedName := types.NamespacedName{
		Namespace: rayJobInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1alpha1.RayCluster{}
	err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance)

	if err == nil {
		if utils.CompareJsonStruct(rayClusterInstance.Spec, rayJobInstance.Spec.RayClusterSpec) {
			return rayClusterInstance, nil
		}
		rayClusterInstance.Spec = rayJobInstance.Spec.RayClusterSpec

		r.Log.Info("Update ray cluster spec", "raycluster", rayClusterNamespacedName)
		if err := r.Update(ctx, rayClusterInstance); err != nil {
			r.Log.Error(err, "Fail to update ray cluster instance!", "rayCluster", rayClusterNamespacedName)
			// Error updating the RayCluster object.
			return nil, client.IgnoreNotFound(err)
		}
	} else if errors.IsNotFound(err) {
		r.Log.Info("Not found rayCluster, creating rayCluster!")
		rayClusterInstance, err = r.constructRayClusterForRayJob(rayJobInstance, rayClusterInstanceName)
		if err != nil {
			r.Log.Error(err, "unable to construct a new rayCluster")
			// Error construct the RayCluster object - requeue the request.
			return nil, err
		}
		if err := r.Create(ctx, rayClusterInstance); err != nil {
			r.Log.Error(err, "unable to create rayCluster for rayJob", "rayCluster", rayClusterInstance)
			// Error creating the RayCluster object - requeue the request.
			return nil, err
		}
		r.Log.Info("created rayCluster for rayJob", "rayCluster", rayClusterInstance)
	} else {
		r.Log.Error(err, "Get rayCluster instance error!")
		// Error reading the RayCluster object - requeue the request.
		return nil, err
	}

	return rayClusterInstance, nil
}

func (r *RayJobReconciler) constructRayClusterForRayJob(rayJobInstance *rayv1alpha1.RayJob, rayClusterName string) (*rayv1alpha1.RayCluster, error) {
	rayCluster := &rayv1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      rayJobInstance.Labels,
			Annotations: rayJobInstance.Annotations,
			Name:        rayClusterName,
			Namespace:   rayJobInstance.Namespace,
		},
		Spec: *rayJobInstance.Spec.RayClusterSpec.DeepCopy(),
	}

	// Set the ownership in order to do the garbage collection by k8s.
	if err := ctrl.SetControllerReference(rayJobInstance, rayCluster, r.Scheme); err != nil {
		return nil, err
	}

	return rayCluster, nil
}
