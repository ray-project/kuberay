package ray

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/tools/record"

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
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
}

// NewRayJobReconciler returns a new reconcile.Reconciler
func NewRayJobReconciler(mgr manager.Manager) *RayJobReconciler {
	return &RayJobReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("RayJob"),
		Recorder: mgr.GetEventRecorderFor("rayjob-controller"),
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
	r.Log.Info("reconciling RayJob", "NamespacedName", request.NamespacedName)

	// Get RayJob instance
	var err error
	var rayJobInstance *rayv1alpha1.RayJob
	if rayJobInstance, err = r.getRayJobInstance(ctx, request); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Do not reconcile the RayJob if the deployment status is marked as Complete
	if rayJobInstance.Status.JobDeploymentStatus == rayv1alpha1.JobDeploymentStatusComplete {
		r.Log.Info("rayjob is complete, skip reconciliation", "rayjob", rayJobInstance.Name)
		return ctrl.Result{}, nil
	}

	// Mark the deployment status as Complete if RayJob is succeed or failed
	// TODO: (jiaxin.shan) Double check raycluster status to make sure we don't have create duplicate clusters..
	// But the code here is not elegant. We should spend some time to refactor the flow.
	if isJobSucceedOrFailed(rayJobInstance.Status.JobStatus) && rayJobInstance.Status.JobDeploymentStatus != rayv1alpha1.JobDeploymentStatusComplete {
		// We need to make sure the cluster is deleted or in deletion, then update the status.
		rayClusterInstance := &rayv1alpha1.RayCluster{}
		rayClusterNamespacedName := types.NamespacedName{
			Namespace: rayJobInstance.Namespace,
			Name:      rayJobInstance.Status.RayClusterName,
		}
		if err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance); err != nil {
			if !errors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			if err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1alpha1.JobDeploymentStatusComplete, nil); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		if rayClusterInstance.DeletionTimestamp != nil {
			if err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1alpha1.JobDeploymentStatusComplete, nil); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Set rayClusterName and rayJobId first, to avoid duplicate submission
	err = r.setRayJobIdAndRayClusterNameIfNeed(ctx, rayJobInstance)
	if err != nil {
		r.Log.Error(err, "failed to set jobId or rayCluster name", "RayJob", request.NamespacedName)
		return ctrl.Result{}, err
	}

	var rayClusterInstance *rayv1alpha1.RayCluster
	if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayJobInstance); err != nil {
		err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1alpha1.JobDeploymentStatusFailedToGetOrCreateRayCluster, err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Always update RayClusterStatus along with jobStatus and jobDeploymentStatus updates.
	rayJobInstance.Status.RayClusterStatus = rayClusterInstance.Status

	clientURL := rayJobInstance.Status.DashboardURL
	if clientURL == "" {
		// TODO: dashboard service may be changed. Check it instead of using the same URL always
		if clientURL, err = utils.FetchDashboardURL(ctx, &r.Log, r.Client, rayClusterInstance); err != nil || clientURL == "" {
			if clientURL == "" {
				err = fmt.Errorf("empty dashboardURL")
			}
			err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1alpha1.JobDeploymentStatusWaitForDashboard, err)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		rayJobInstance.Status.DashboardURL = clientURL
	}

	rayDashboardClient := utils.GetRayDashboardClientFunc()
	rayDashboardClient.InitClient(clientURL)

	// Check the current status of ray cluster before submitting.
	if rayClusterInstance.Status.State != rayv1alpha1.Ready {
		r.Log.Info("waiting for the cluster to be ready", "rayCluster", rayClusterInstance.Name)
		err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1alpha1.JobDeploymentStatusInitializing, nil)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Check the current status of ray jobs before submitting.
	jobInfo, err := rayDashboardClient.GetJobInfo(rayJobInstance.Status.JobId)
	if err != nil {
		err = r.updateState(ctx, rayJobInstance, jobInfo, rayJobInstance.Status.JobStatus, rayv1alpha1.JobDeploymentStatusFailedToGetJobStatus, err)
		// Dashboard service in head pod takes time to start, it's possible we get connection refused error.
		// Requeue after few seconds to avoid continuous connection errors.
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	r.Log.V(1).Info("RayJob information", "RayJob", rayJobInstance.Name, "jobInfo", jobInfo, "rayJobInstance", rayJobInstance.Status.JobStatus)
	if jobInfo == nil {
		// Submit the job if no id set
		jobId, err := rayDashboardClient.SubmitJob(rayJobInstance, &r.Log)
		if err != nil {
			r.Log.Error(err, "failed to submit job")
			err = r.updateState(ctx, rayJobInstance, jobInfo, rayJobInstance.Status.JobStatus, rayv1alpha1.JobDeploymentStatusFailedJobDeploy, err)
			return ctrl.Result{}, err
		}

		r.Log.Info("Job successfully submitted", "RayJob", rayJobInstance.Name, "jobId", jobId)
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Submitted", "Submit Job %s", jobId)
		// Here, we directly update to PENDING and emit an event to trigger a new reconcile loop
		err = r.updateState(ctx, rayJobInstance, jobInfo, rayv1alpha1.JobStatusPending, rayv1alpha1.JobDeploymentStatusRunning, nil)
		if err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		return ctrl.Result{}, nil
	}

	// Update RayJob.Status (Kubernetes CR) from Ray Job Status from Dashboard service
	if jobInfo.JobStatus != rayJobInstance.Status.JobStatus {
		r.Log.Info(fmt.Sprintf("Update status from %s to %s", rayJobInstance.Status.JobStatus, jobInfo.JobStatus), "rayjob", rayJobInstance.Status.JobId)
		err = r.updateState(ctx, rayJobInstance, jobInfo, jobInfo.JobStatus, rayv1alpha1.JobDeploymentStatusRunning, nil)
		return ctrl.Result{}, err
	}

	// Job may takes long time to start and finish, let's just periodically requeue the job and check status.
	if isJobPendingOrRunning(jobInfo.JobStatus) && rayJobInstance.Status.JobDeploymentStatus == rayv1alpha1.JobDeploymentStatusRunning {
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	}

	// Let's use rayJobInstance.Status.JobStatus to make sure we only delete cluster after the CR is updated.
	if isJobSucceedOrFailed(rayJobInstance.Status.JobStatus) && rayJobInstance.Status.JobDeploymentStatus == rayv1alpha1.JobDeploymentStatusRunning {
		if rayJobInstance.Spec.ShutdownAfterJobFinishes {
			r.Log.V(3).Info("TTLSecondsAfterSetting", "end_time", rayJobInstance.Status.EndTime.Time, "now", time.Now(), "ttl", *rayJobInstance.Spec.TTLSecondsAfterFinished)
			if rayJobInstance.Spec.TTLSecondsAfterFinished != nil {
				ttlDuration := time.Duration(*rayJobInstance.Spec.TTLSecondsAfterFinished) * time.Second
				if rayJobInstance.Status.EndTime.Time.Add(ttlDuration).After(time.Now()) {
					// time.Until prints duration until target time. We add additional 2 seconds to make sure we have buffer and requeueAfter is not 0.
					delta := int32(time.Until(rayJobInstance.Status.EndTime.Time.Add(ttlDuration).Add(2 * time.Second)).Seconds())
					r.Log.Info("TTLSecondsAfterFinish not reached, requeue it after", "RayJob", rayJobInstance.Name, "time(s)", delta)
					return ctrl.Result{RequeueAfter: time.Duration(delta) * time.Second}, nil
				}
			}

			r.Log.Info("shutdownAfterJobFinishes set to true, we will delete cluster",
				"RayJob", rayJobInstance.Name, "clusterName", fmt.Sprintf("%s/%s", rayJobInstance.Namespace, rayJobInstance.Status.RayClusterName))
			clusterIdentifier := types.NamespacedName{
				Name:      rayJobInstance.Status.RayClusterName,
				Namespace: rayJobInstance.Namespace,
			}
			cluster := rayv1alpha1.RayCluster{}
			if err := r.Get(ctx, clusterIdentifier, &cluster); err != nil {
				if !errors.IsNotFound(err) {
					return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
				}
				r.Log.Info("The associated cluster has been already deleted and it can not be found", "RayCluster", clusterIdentifier)
			} else {
				if cluster.DeletionTimestamp != nil {
					r.Log.Info("The cluster deletion is ongoing.", "rayjob", rayJobInstance.Name, "raycluster", cluster.Name)
				} else {
					if err := r.Delete(ctx, &cluster); err != nil {
						return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
					}
					r.Log.Info("The associated cluster is deleted", "RayCluster", clusterIdentifier)
					r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Deleted", "Deleted cluster %s", rayJobInstance.Status.RayClusterName)
					return ctrl.Result{Requeue: true}, nil
				}
			}
		}
	}

	return ctrl.Result{}, nil
}

// isJobSucceedOrFailed indicates whether the job comes into end status.
func isJobSucceedOrFailed(status rayv1alpha1.JobStatus) bool {
	if status == rayv1alpha1.JobStatusSucceeded || status == rayv1alpha1.JobStatusFailed {
		return true
	}
	return false
}

// isJobPendingOrRunning indicates whether the job is running.
func isJobPendingOrRunning(status rayv1alpha1.JobStatus) bool {
	if status == rayv1alpha1.JobStatusPending || status == rayv1alpha1.JobStatusRunning {
		return true
	}
	return false
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
			r.Log.Info("Read request instance not found error!", "name", request.NamespacedName)
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
		return r.updateState(ctx, rayJob, nil, rayJob.Status.JobStatus, rayv1alpha1.JobDeploymentStatusInitializing, nil)
	}
	return nil
}

// make sure the priority is correct
func (r *RayJobReconciler) updateState(ctx context.Context, rayJob *rayv1alpha1.RayJob, jobInfo *utils.RayJobInfo, jobStatus rayv1alpha1.JobStatus, jobDeploymentStatus rayv1alpha1.JobDeploymentStatus, err error) error {
	// Let's skip update the APIServer if it's synced.
	if rayJob.Status.JobStatus == jobStatus && rayJob.Status.JobDeploymentStatus == jobDeploymentStatus {
		return nil
	}

	r.Log.Info("UpdateState", "oldJobStatus", rayJob.Status.JobStatus, "newJobStatus", jobStatus, "oldJobDeploymentStatus", rayJob.Status.JobDeploymentStatus, "newJobDeploymentStatus", jobDeploymentStatus)
	rayJob.Status.JobStatus = jobStatus
	rayJob.Status.JobDeploymentStatus = jobDeploymentStatus
	if jobInfo != nil {
		rayJob.Status.Message = jobInfo.Message
		rayJob.Status.StartTime = utils.ConvertUnixTimeToMetav1Time(jobInfo.StartTime)
		rayJob.Status.EndTime = utils.ConvertUnixTimeToMetav1Time(jobInfo.EndTime)
	}

	if errStatus := r.Status().Update(ctx, rayJob); errStatus != nil {
		return fmtErrors.Errorf("combined error: %v %v", err, errStatus)
	}
	return err
}

// TODO: select existing rayclusters by ClusterSelector
func (r *RayJobReconciler) getOrCreateRayClusterInstance(ctx context.Context, rayJobInstance *rayv1alpha1.RayJob) (*rayv1alpha1.RayCluster, error) {
	rayClusterInstanceName := rayJobInstance.Status.RayClusterName
	r.Log.V(3).Info("try to find existing RayCluster instance", "name", rayClusterInstanceName)
	rayClusterNamespacedName := types.NamespacedName{
		Namespace: rayJobInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1alpha1.RayCluster{}
	err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance)
	if err == nil {
		r.Log.Info("RayJob associated rayCluster found", "rayjob", rayJobInstance.Name, "raycluster", rayClusterNamespacedName)
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
		// one special case is the job is complete status and cluster has been recycled.
		if isJobSucceedOrFailed(rayJobInstance.Status.JobStatus) && rayJobInstance.Status.JobDeploymentStatus == rayv1alpha1.JobDeploymentStatusComplete {
			r.Log.Info("The cluster has been recycled for the job, skip duplicate creation", "rayjob", rayJobInstance.Name)
			return nil, err
		}

		r.Log.Info("RayCluster not found, creating rayCluster!", "raycluster", rayClusterNamespacedName)
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
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Created", "Created cluster %s", rayJobInstance.Status.RayClusterName)
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
