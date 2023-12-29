package ray

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

const (
	RayJobDefaultRequeueDuration    = 3 * time.Second
	RayJobDefaultClusterSelectorKey = "ray.io/cluster"
	PythonUnbufferedEnvVarName      = "PYTHONUNBUFFERED"
)

// RayJobReconciler reconciles a RayJob object
type RayJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder

	dashboardClientFunc func() utils.RayDashboardClientInterface
}

// NewRayJobReconciler returns a new reconcile.Reconciler
func NewRayJobReconciler(mgr manager.Manager, dashboardClientFunc func() utils.RayDashboardClientInterface) *RayJobReconciler {
	return &RayJobReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("RayJob"),
		Recorder: mgr.GetEventRecorderFor("rayjob-controller"),

		dashboardClientFunc: dashboardClientFunc,
	}
}

// +kubebuilder:rbac:groups=ray.io,resources=rayjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;delete;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// [WARNING]: There MUST be a newline after kubebuilder markers.
// Reconcile reads that state of a RayJob object and makes changes based on it
// and what is in the RayJob.Spec
// Automatically generate RBAC rules to allow the Controller to read and write workloads
// Reconcile used to bridge the desired state with the current state
func (r *RayJobReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling RayJob", "NamespacedName", request.NamespacedName)

	// Get RayJob instance
	var err error
	rayJobInstance := &rayv1.RayJob{}
	if err := r.Get(ctx, request.NamespacedName, rayJobInstance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request. Stop reconciliation.
			r.Log.Info("RayJob resource not found. Ignoring since object must be deleted", "name", request.NamespacedName)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get RayJob")
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	if !rayJobInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Log.Info("RayJob is being deleted", "DeletionTimestamp", rayJobInstance.ObjectMeta.DeletionTimestamp)
		if isJobPendingOrRunning(rayJobInstance.Status.JobStatus) {
			rayDashboardClient := r.dashboardClientFunc()
			rayDashboardClient.InitClient(rayJobInstance.Status.DashboardURL)
			err := rayDashboardClient.StopJob(ctx, rayJobInstance.Status.JobId, &r.Log)
			if err != nil {
				r.Log.Info("Failed to stop job for RayJob", "error", err)
			}
		}

		r.Log.Info("Remove the finalizer no matter StopJob() succeeds or not.", "finalizer", utils.RayJobStopJobFinalizer)
		controllerutil.RemoveFinalizer(rayJobInstance, utils.RayJobStopJobFinalizer)
		err := r.Update(ctx, rayJobInstance)
		if err != nil {
			r.Log.Error(err, "Failed to remove finalizer for RayJob")
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	r.Log.Info("RayJob", "name", rayJobInstance.Name, "namespace", rayJobInstance.Namespace, "JobStatus", rayJobInstance.Status.JobStatus, "JobDeploymentStatus", rayJobInstance.Status.JobDeploymentStatus)
	switch rayJobInstance.Status.JobDeploymentStatus {
	case rayv1.JobDeploymentStatusNew:
		// TODO (kevin85421): Write a utility function to add finalizer for both RayJob and RayCluster.
		if !controllerutil.ContainsFinalizer(rayJobInstance, utils.RayJobStopJobFinalizer) {
			r.Log.Info("Add a finalizer", "finalizer", utils.RayJobStopJobFinalizer)
			controllerutil.AddFinalizer(rayJobInstance, utils.RayJobStopJobFinalizer)
			if err := r.Update(ctx, rayJobInstance); err != nil {
				r.Log.Error(err, "Failed to update RayJob with finalizer")
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
		}
		// Set `Status.JobDeploymentStatus` to `JobDeploymentStatusInitializing`, and initialize `Status.JobId`
		// and `Status.RayClusterName` prior to avoid duplicate job submissions and cluster creations.
		r.Log.Info("JobDeploymentStatusNew", "RayJob", rayJobInstance.Name)
		if err = r.initRayJobStatusIfNeed(ctx, rayJobInstance); err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	case rayv1.JobDeploymentStatusRunning:
		// If the JobStatus is in the SUCCEEDED or FAILED, it is impossible for the Ray job to transition to any other status
		// because both of them are terminal status. Additionally, RayJob does not currently support retries. Hence, we can
		// mark the RayJob as "Complete" to avoid unnecessary reconciliation. Note that the definition of "Complete" does not
		// include STOPPED which is also a terminal status because `suspend` requires to stop the Ray job gracefully before
		// delete the RayCluster.
		if isJobSucceedOrFail(rayJobInstance.Status.JobStatus) {
			if err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusComplete); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
		}
		// TODO (kevin85421): Don't return here; we still need to move some of the code below into this block.
	case rayv1.JobDeploymentStatusSuspended:
		if !rayJobInstance.Spec.Suspend {
			r.Log.Info("The status is 'Suspended', but the suspend flag is false. Transition the status to 'New'.")
			if err = r.updateState(ctx, rayJobInstance, nil, rayv1.JobStatusNew, rayv1.JobDeploymentStatusNew); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
		}
		// TODO (kevin85421): We may not need to requeue the RayJob if it has already been suspended.
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	case rayv1.JobDeploymentStatusComplete:
		// If this RayJob uses an existing RayCluster (i.e., ClusterSelector is set), we should not delete the RayCluster.
		r.Log.Info("JobDeploymentStatusComplete", "RayJob", rayJobInstance.Name, "ShutdownAfterJobFinishes", rayJobInstance.Spec.ShutdownAfterJobFinishes, "ClusterSelector", rayJobInstance.Spec.ClusterSelector)
		if rayJobInstance.Spec.ShutdownAfterJobFinishes && len(rayJobInstance.Spec.ClusterSelector) == 0 {
			// TODO (kevin85421): Revisit EndTime and ensure it will always be set after the job is completed.
			ttlSeconds := rayJobInstance.Spec.TTLSecondsAfterFinished
			nowTime := time.Now()
			shutdownTime := rayJobInstance.Status.EndTime.Add(time.Duration(ttlSeconds) * time.Second)
			r.Log.Info(
				"RayJob is completed",
				"shutdownAfterJobFinishes", rayJobInstance.Spec.ShutdownAfterJobFinishes,
				"ttlSecondsAfterFinished", ttlSeconds,
				"Status.endTime", rayJobInstance.Status.EndTime,
				"Now", nowTime,
				"ShutdownTime", shutdownTime)
			if shutdownTime.After(nowTime) {
				delta := int32(time.Until(shutdownTime.Add(2 * time.Second)).Seconds())
				r.Log.Info(fmt.Sprintf("shutdownTime not reached, requeue this RayJob for %d seconds", delta))
				return ctrl.Result{RequeueAfter: time.Duration(delta) * time.Second}, nil
			} else {
				if err = r.releaseComputeResources(ctx, rayJobInstance, false); err != nil {
					return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
				}
			}
		}
		// If the RayJob is completed, we should not requeue it.
		return ctrl.Result{}, nil
	}

	var rayClusterInstance *rayv1.RayCluster
	if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayJobInstance); err != nil {
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}
	// If there is no cluster instance and no error suspend the job deployment
	if rayClusterInstance == nil {
		err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusSuspended)
		if err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		r.Log.Info("rayJob suspended", "RayJob", rayJobInstance.Name)
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Suspended", "Suspended RayJob %s", rayJobInstance.Name)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Always update RayClusterStatus along with jobStatus and jobDeploymentStatus updates.
	rayJobInstance.Status.RayClusterStatus = rayClusterInstance.Status
	rayDashboardClient := r.dashboardClientFunc()

	// Check the current status of ray cluster before submitting.
	if clientURL := rayJobInstance.Status.DashboardURL; clientURL == "" {
		if rayClusterInstance.Status.State != rayv1.Ready {
			r.Log.Info("Wait for the RayCluster.Status.State to be ready before submitting the job.", "RayCluster", rayClusterInstance.Name, "State", rayClusterInstance.Status.State)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}

		if clientURL, err = utils.FetchHeadServiceURL(ctx, &r.Log, r.Client, rayClusterInstance, utils.DashboardPortName); err != nil || clientURL == "" {
			r.Log.Error(err, "Failed to get the dashboard URL after the RayCluster is ready!", "RayCluster", rayClusterInstance.Name)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		rayJobInstance.Status.DashboardURL = clientURL

		// TODO (kevin85421): The function `updateState` might skip the update if `JobStatus` and `JobDeploymentStatus`
		// are already identical to the given values. Therefore, we use `r.Status().Update()` to directly update the status.
		// However, neither `updateState` nor `r.Status().Update()` is an ideal solution. We should refactor the code.
		if err := r.Status().Update(ctx, rayJobInstance); err != nil {
			r.Log.Error(err, "Failed to update the dashboard URL to the RayJob status!", "RayJob", rayJobInstance.Name)
		}
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}
	rayDashboardClient.InitClient(rayJobInstance.Status.DashboardURL)

	// Ensure k8s job has been created
	if err := r.createK8sJobIfNeed(ctx, rayJobInstance, rayClusterInstance); err != nil {
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Check the current status of ray jobs
	jobInfo, err := rayDashboardClient.GetJobInfo(ctx, rayJobInstance.Status.JobId)
	if err != nil {
		r.Log.Error(err, "failed to get job info", "jobId", rayJobInstance.Status.JobId)
		// Dashboard service in head pod takes time to start, it's possible we get connection refused error.
		// Requeue after few seconds to avoid continuous connection errors.
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}
	r.Log.Info("GetJobInfo", "Job Info", jobInfo)

	err = r.updateState(ctx, rayJobInstance, jobInfo, jobInfo.JobStatus, rayv1.JobDeploymentStatusRunning)
	if err != nil {
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	if rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusRunning {
		// If suspend flag is set AND
		// the RayJob is submitted against the RayCluster created by THIS job, then
		// try to gracefully stop the Ray job and delete (suspend) the cluster
		if rayJobInstance.Spec.Suspend && len(rayJobInstance.Spec.ClusterSelector) == 0 {
			if !rayv1.IsJobTerminal(jobInfo.JobStatus) {
				err := rayDashboardClient.StopJob(ctx, rayJobInstance.Status.JobId, &r.Log)
				if err != nil {
					return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
				}
			}
			if jobInfo.JobStatus != rayv1.JobStatusStopped {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
			}

			if err = r.releaseComputeResources(ctx, rayJobInstance, true); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			// Since RayCluster instance is gone, remove it status also
			// on RayJob resource
			rayJobInstance.Status.RayClusterStatus = rayv1.RayClusterStatus{}
			rayJobInstance.Status.RayClusterName = ""
			rayJobInstance.Status.DashboardURL = ""
			rayJobInstance.Status.JobId = ""
			rayJobInstance.Status.Message = ""
			err = r.updateState(ctx, rayJobInstance, jobInfo, rayv1.JobStatusStopped, rayv1.JobDeploymentStatusSuspended)
			if err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			r.Log.Info("rayJob suspended", "RayJob", rayJobInstance.Name)
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Suspended", "Suspended RayJob %s", rayJobInstance.Name)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
			// Job may takes long time to start and finish, let's just periodically requeue the job and check status.
		}
	}

	return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
}

// createK8sJobIfNeed creates a Kubernetes Job for the RayJob if it doesn't exist.
func (r *RayJobReconciler) createK8sJobIfNeed(ctx context.Context, rayJobInstance *rayv1.RayJob, rayClusterInstance *rayv1.RayCluster) error {
	jobName := rayJobInstance.Name
	jobNamespace := rayJobInstance.Namespace

	// Create a Job object with the specified name and namespace
	job := &batchv1.Job{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: jobNamespace, Name: jobName}, job); err != nil {
		if errors.IsNotFound(err) {
			submitterTemplate, err := r.getSubmitterTemplate(rayJobInstance, rayClusterInstance)
			if err != nil {
				r.Log.Error(err, "failed to get submitter template")
				return err
			}
			return r.createNewK8sJob(ctx, rayJobInstance, submitterTemplate)
		}

		// Some other error occurred while trying to get the Job
		r.Log.Error(err, "failed to get Kubernetes Job")
		return err
	}

	r.Log.Info("Kubernetes Job already exists", "RayJob", rayJobInstance.Name, "Kubernetes Job", job.Name)
	return nil
}

// getSubmitterTemplate builds the submitter pod template for the Ray job.
func (r *RayJobReconciler) getSubmitterTemplate(rayJobInstance *rayv1.RayJob, rayClusterInstance *rayv1.RayCluster) (corev1.PodTemplateSpec, error) {
	var submitterTemplate corev1.PodTemplateSpec

	// Set the default value for the optional field SubmitterPodTemplate if not provided.
	if rayJobInstance.Spec.SubmitterPodTemplate == nil {
		submitterTemplate = common.GetDefaultSubmitterTemplate(rayClusterInstance)
		r.Log.Info("default submitter template is used")
	} else {
		submitterTemplate = *rayJobInstance.Spec.SubmitterPodTemplate.DeepCopy()
		r.Log.Info("user-provided submitter template is used; the first container is assumed to be the submitter")
	}

	// If the command in the submitter pod template isn't set, use the default command.
	if len(submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command) == 0 {
		// Check for deprecated 'runtimeEnv' field usage and log a warning.
		if len(rayJobInstance.Spec.RuntimeEnv) > 0 {
			r.Log.Info("Warning: The 'runtimeEnv' field is deprecated. Please use 'runtimeEnvYAML' instead.")
		}

		k8sJobCommand, err := common.GetK8sJobCommand(rayJobInstance)
		if err != nil {
			return corev1.PodTemplateSpec{}, err
		}
		submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command = k8sJobCommand
		r.Log.Info("No command is specified in the user-provided template. Default command is used", "command", k8sJobCommand)
	} else {
		r.Log.Info("User-provided command is used", "command", submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command)
	}

	// Set PYTHONUNBUFFERED=1 for real-time logging
	submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env = append(submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env, corev1.EnvVar{
		Name:  PythonUnbufferedEnvVarName,
		Value: "1",
	})

	return submitterTemplate, nil
}

// createNewK8sJob creates a new Kubernetes Job. It returns an error.
func (r *RayJobReconciler) createNewK8sJob(ctx context.Context, rayJobInstance *rayv1.RayJob, submitterTemplate corev1.PodTemplateSpec) error {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rayJobInstance.Name,
			Namespace: rayJobInstance.Namespace,
			Labels: map[string]string{
				utils.KubernetesCreatedByLabelKey: utils.ComponentName,
			},
		},
		Spec: batchv1.JobSpec{
			// Reduce the number of retries, which defaults to 6, so the ray job submission command
			// is attempted 3 times at the maximum, but still mitigates the case of unrecoverable
			// application-level errors, where the maximum number of retries is reached, and the job
			// completion time increases with no benefits, but wasted resource cycles.
			BackoffLimit: pointer.Int32(2),
			Template:     submitterTemplate,
		},
	}

	// Without TTLSecondsAfterFinished, the job has a default deletion policy of `orphanDependents` causing
	// Pods created by an unmanaged Job to be left around after that Job is fully deleted.
	if rayJobInstance.Spec.ShutdownAfterJobFinishes {
		job.Spec.TTLSecondsAfterFinished = pointer.Int32(rayJobInstance.Spec.TTLSecondsAfterFinished)
	}

	// Set the ownership in order to do the garbage collection by k8s.
	if err := ctrl.SetControllerReference(rayJobInstance, job, r.Scheme); err != nil {
		r.Log.Error(err, "failed to set controller reference")
		return err
	}

	// Create the Kubernetes Job
	if err := r.Client.Create(ctx, job); err != nil {
		r.Log.Error(err, "failed to create k8s Job")
		return err
	}
	r.Log.Info("Kubernetes Job created", "RayJob", rayJobInstance.Name, "Kubernetes Job", job.Name)
	r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Created", "Created Kubernetes Job %s", job.Name)
	return nil
}

// Delete the RayCluster associated with the RayJob to release the compute resources.
// In the future, we may also need to delete other Kubernetes resources. Note that
// this function doesn't delete the Kubernetes Job. Instead, we use the built-in
// TTL mechanism of the Kubernetes Job for deletion.
func (r *RayJobReconciler) releaseComputeResources(ctx context.Context, rayJobInstance *rayv1.RayJob, isSuspend bool) error {
	namespace := rayJobInstance.Namespace
	clusterIdentifier := types.NamespacedName{
		Name:      rayJobInstance.Status.RayClusterName,
		Namespace: namespace,
	}
	cluster := rayv1.RayCluster{}
	if err := r.Get(ctx, clusterIdentifier, &cluster); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// If the cluster is not found, it means the cluster has been already deleted.
		// Don't return error to make this function idempotent.
		r.Log.Info("The associated cluster has been already deleted and it can not be found", "RayCluster", clusterIdentifier)
	} else {
		if cluster.DeletionTimestamp != nil {
			r.Log.Info("The cluster deletion is ongoing.", "rayjob", rayJobInstance.Name, "raycluster", cluster.Name)
		} else {
			if err := r.Delete(ctx, &cluster); err != nil {
				return err
			}
			r.Log.Info("The associated cluster is deleted", "RayCluster", clusterIdentifier)
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Deleted", "Deleted cluster %s", rayJobInstance.Status.RayClusterName)
		}
	}

	// Since the name of the Kubernetes Job is the same as the RayJob, we need to set the TTL of the Kubernetes Job to clean
	// up the Kubernetes Job and its Pods when suspending, and a new submitter Kubernetes Job must be created to resubmit the
	// Ray job if the RayJob is resumed.
	if isSuspend {
		jobName := rayJobInstance.Name
		job := &batchv1.Job{}
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: jobName}, job); err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("The submitter Kubernetes Job has been already deleted", "RayJob", rayJobInstance.Name, "Kubernetes Job", job.Name)
			} else {
				r.Log.Error(err, "Failed to get Kubernetes Job")
				return err
			}
		}
		job.Spec.TTLSecondsAfterFinished = pointer.Int32(0)
		if err := r.Client.Update(ctx, job); err != nil {
			r.Log.Error(err, "Failed to update the TTL of the Kubernetes Job")
			return err
		}
	}
	return nil
}

// isJobSucceedOrFail indicates whether the job comes into end status.
func isJobSucceedOrFail(status rayv1.JobStatus) bool {
	return (status == rayv1.JobStatusSucceeded) || (status == rayv1.JobStatusFailed)
}

// isJobPendingOrRunning indicates whether the job is running.
func isJobPendingOrRunning(status rayv1.JobStatus) bool {
	return (status == rayv1.JobStatusPending) || (status == rayv1.JobStatusRunning)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayJob{}).
		Owns(&rayv1.RayCluster{}).
		Owns(&corev1.Service{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

// This function is the sole place where `JobDeploymentStatusInitializing` is defined. It initializes `Status.JobId` and `Status.RayClusterName`
// prior to job submissions and RayCluster creations. This is used to avoid duplicate job submissions and cluster creations.
func (r *RayJobReconciler) initRayJobStatusIfNeed(ctx context.Context, rayJob *rayv1.RayJob) error {
	shouldUpdateStatus := rayJob.Status.JobId == "" || rayJob.Status.RayClusterName == "" || rayJob.Status.JobStatus == ""
	// Please don't update `shouldUpdateStatus` below.
	r.Log.Info("initRayJobStatusIfNeed", "shouldUpdateStatus", shouldUpdateStatus, "RayJob", rayJob.Name, "jobId", rayJob.Status.JobId, "rayClusterName", rayJob.Status.RayClusterName, "jobStatus", rayJob.Status.JobStatus)
	if !shouldUpdateStatus {
		return nil
	}

	if rayJob.Status.JobId == "" {
		if rayJob.Spec.JobId != "" {
			rayJob.Status.JobId = rayJob.Spec.JobId
		} else {
			rayJob.Status.JobId = utils.GenerateRayJobId(rayJob.Name)
		}
	}

	if rayJob.Status.RayClusterName == "" {
		// if the clusterSelector is not empty, default use this cluster name
		// we assume the length of clusterSelector is one
		if len(rayJob.Spec.ClusterSelector) != 0 {
			var useValue string
			var ok bool
			if useValue, ok = rayJob.Spec.ClusterSelector[RayJobDefaultClusterSelectorKey]; !ok {
				return fmt.Errorf("failed to get cluster name in ClusterSelector map, the default key is %v", RayJobDefaultClusterSelectorKey)
			}
			rayJob.Status.RayClusterName = useValue
		} else {
			rayJob.Status.RayClusterName = utils.GenerateRayClusterName(rayJob.Name)
		}
	}

	if rayJob.Status.JobStatus == "" {
		rayJob.Status.JobStatus = rayv1.JobStatusNew
	}

	return r.updateState(ctx, rayJob, nil, rayJob.Status.JobStatus, rayv1.JobDeploymentStatusInitializing)
}

// make sure the priority is correct
func (r *RayJobReconciler) updateState(ctx context.Context, rayJob *rayv1.RayJob, jobInfo *utils.RayJobInfo, jobStatus rayv1.JobStatus, jobDeploymentStatus rayv1.JobDeploymentStatus) error {
	r.Log.Info("UpdateState", "oldJobStatus", rayJob.Status.JobStatus, "newJobStatus", jobStatus, "oldJobDeploymentStatus", rayJob.Status.JobDeploymentStatus, "newJobDeploymentStatus", jobDeploymentStatus)

	// Let's skip update the APIServer if it's synced.
	if rayJob.Status.JobStatus == jobStatus && rayJob.Status.JobDeploymentStatus == jobDeploymentStatus {
		return nil
	}

	rayJob.Status.JobStatus = jobStatus
	rayJob.Status.JobDeploymentStatus = jobDeploymentStatus
	if jobInfo != nil {
		rayJob.Status.Message = jobInfo.Message
		rayJob.Status.StartTime = utils.ConvertUnixTimeToMetav1Time(jobInfo.StartTime)
		if jobInfo.StartTime >= jobInfo.EndTime {
			rayJob.Status.EndTime = nil
		} else {
			rayJob.Status.EndTime = utils.ConvertUnixTimeToMetav1Time(jobInfo.EndTime)
		}
	}

	// TODO (kevin85421): ObservedGeneration should be used to determine whether update this CR or not.
	rayJob.Status.ObservedGeneration = rayJob.ObjectMeta.Generation

	if err := r.Status().Update(ctx, rayJob); err != nil {
		return err
	}
	return nil
}

func (r *RayJobReconciler) getOrCreateRayClusterInstance(ctx context.Context, rayJobInstance *rayv1.RayJob) (*rayv1.RayCluster, error) {
	rayClusterInstanceName := rayJobInstance.Status.RayClusterName
	r.Log.Info("try to find existing RayCluster instance", "name", rayClusterInstanceName)
	rayClusterNamespacedName := types.NamespacedName{
		Namespace: rayJobInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1.RayCluster{}
	if err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("RayCluster not found", "RayJob", rayJobInstance.Name, "RayCluster", rayClusterNamespacedName)
			// TODO: If both ClusterSelector and RayClusterSpec are not set, we should avoid retrieving a RayCluster instance.
			// Consider moving this logic to a more appropriate location.
			if len(rayJobInstance.Spec.ClusterSelector) == 0 && rayJobInstance.Spec.RayClusterSpec == nil {
				err := fmt.Errorf("one of ClusterSelector or RayClusterSpec must be set, but both are undefined, err: %v", err)
				return nil, err
			}

			if len(rayJobInstance.Spec.ClusterSelector) != 0 {
				err := fmt.Errorf("we have choosed the cluster selector mode, failed to find the cluster named %v, err: %v", rayClusterInstanceName, err)
				return nil, err
			}

			// special case: don't create a cluster instance and don't return an error if the suspend flag of the job is true
			if rayJobInstance.Spec.Suspend {
				return nil, nil
			}

			r.Log.Info("RayCluster not found, creating RayCluster!", "RayCluster", rayClusterNamespacedName)
			rayClusterInstance, err = r.constructRayClusterForRayJob(rayJobInstance, rayClusterInstanceName)
			if err != nil {
				r.Log.Error(err, "unable to construct a new RayCluster")
				return nil, err
			}
			if err := r.Create(ctx, rayClusterInstance); err != nil {
				r.Log.Error(err, "unable to create RayCluster for RayJob", "RayCluster", rayClusterInstance)
				return nil, err
			}
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Created", "Created RayCluster %s", rayJobInstance.Status.RayClusterName)
		} else {
			r.Log.Error(err, "Fail to get RayCluster!")
			return nil, err
		}
	}
	r.Log.Info("Found associated RayCluster for RayJob", "RayJob", rayJobInstance.Name, "RayCluster", rayClusterNamespacedName)

	// Verify that RayJob is not in cluster selector mode first to avoid nil pointer dereference error during spec comparison.
	// This is checked by ensuring len(rayJobInstance.Spec.ClusterSelector) equals 0.
	if len(rayJobInstance.Spec.ClusterSelector) == 0 && !utils.CompareJsonStruct(rayClusterInstance.Spec, *rayJobInstance.Spec.RayClusterSpec) {
		r.Log.Info("Disregard changes in RayClusterSpec of RayJob", "RayJob", rayJobInstance.Name)
	}

	return rayClusterInstance, nil
}

func (r *RayJobReconciler) constructRayClusterForRayJob(rayJobInstance *rayv1.RayJob, rayClusterName string) (*rayv1.RayCluster, error) {
	rayCluster := &rayv1.RayCluster{
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
