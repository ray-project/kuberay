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
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		Log:                 ctrl.Log.WithName("controllers").WithName("RayJob"),
		Recorder:            mgr.GetEventRecorderFor("rayjob-controller"),
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
	ctx = ctrl.LoggerInto(ctx, r.Log)
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
		// If the JobStatus is not terminal, it is possible that the Ray job is still running. This includes
		// the case where JobStatus is JobStatusNew.
		if !rayv1.IsJobTerminal(rayJobInstance.Status.JobStatus) {
			rayDashboardClient := r.dashboardClientFunc()
			rayDashboardClient.InitClient(rayJobInstance.Status.DashboardURL)
			err := rayDashboardClient.StopJob(ctx, rayJobInstance.Status.JobId)
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

	if err := validateRayJobSpec(rayJobInstance); err != nil {
		r.Log.Error(err, "The RayJob spec is invalid")
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Please do NOT modify `originalRayJobInstance` in the following code.
	originalRayJobInstance := rayJobInstance.DeepCopy()

	r.Log.Info("RayJob", "name", rayJobInstance.Name, "namespace", rayJobInstance.Namespace, "JobStatus", rayJobInstance.Status.JobStatus, "JobDeploymentStatus", rayJobInstance.Status.JobDeploymentStatus, "SubmissionMode", rayJobInstance.Spec.SubmissionMode)
	switch rayJobInstance.Status.JobDeploymentStatus {
	case rayv1.JobDeploymentStatusNew:
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
	case rayv1.JobDeploymentStatusInitializing:
		if shouldUpdate := r.updateStatusToSuspendingIfNeeded(ctx, rayJobInstance); shouldUpdate {
			break
		}

		var rayClusterInstance *rayv1.RayCluster
		if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayJobInstance); err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}

		// Check the current status of RayCluster before submitting.
		if clientURL := rayJobInstance.Status.DashboardURL; clientURL == "" {
			if rayClusterInstance.Status.State != rayv1.Ready {
				r.Log.Info("Wait for the RayCluster.Status.State to be ready before submitting the job.", "RayCluster", rayClusterInstance.Name, "State", rayClusterInstance.Status.State)
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}

			if clientURL, err = utils.FetchHeadServiceURL(ctx, r.Client, rayClusterInstance, utils.DashboardPortName); err != nil || clientURL == "" {
				r.Log.Error(err, "Failed to get the dashboard URL after the RayCluster is ready!", "RayCluster", rayClusterInstance.Name)
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			rayJobInstance.Status.DashboardURL = clientURL
		}

		if rayJobInstance.Spec.SubmissionMode == rayv1.K8sJobMode {
			if err := r.createK8sJobIfNeed(ctx, rayJobInstance, rayClusterInstance); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
		}

		r.Log.Info("Both RayCluster and the submitter K8s Job are created. Transition the status from `Initializing` to `Running`.",
			"RayJob", rayJobInstance.Name, "RayCluster", rayJobInstance.Status.RayClusterName)
		rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusRunning
	case rayv1.JobDeploymentStatusRunning:
		if shouldUpdate := r.updateStatusToSuspendingIfNeeded(ctx, rayJobInstance); shouldUpdate {
			break
		}

		// TODO (kevin85421): For light-weight mode, calculate the number of failed retries and transition
		// the status to `Complete` if the number of failed retries exceeds the threshold.
		if rayJobInstance.Spec.SubmissionMode == rayv1.K8sJobMode {
			// If the Job reaches the backoff limit, transition the status to `Complete`.
			job := &batchv1.Job{}
			namespacedName := getK8sJobNamespacedName(rayJobInstance)
			if err := r.Client.Get(ctx, namespacedName, job); err != nil {
				r.Log.Error(err, "Failed to get the submitter Kubernetes Job", "NamespacedName", namespacedName)
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			if shouldUpdate := r.checkK8sJobAndUpdateStatusIfNeeded(ctx, rayJobInstance, job); shouldUpdate {
				break
			}
		}

		var rayClusterInstance *rayv1.RayCluster
		// TODO (kevin85421): Maybe we only need to `get` the RayCluster because the RayCluster should have been created
		// before transitioning the status from `Initializing` to `Running`.
		if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayJobInstance); err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}

		// Check the current status of ray jobs
		rayDashboardClient := r.dashboardClientFunc()
		rayDashboardClient.InitClient(rayJobInstance.Status.DashboardURL)
		jobInfo, err := rayDashboardClient.GetJobInfo(ctx, rayJobInstance.Status.JobId)
		if err != nil {
			// If the Ray job was not found, GetJobInfo returns a BadRequest error.
			if rayJobInstance.Spec.SubmissionMode == rayv1.HTTPMode && errors.IsBadRequest(err) {
				r.Log.Info("The Ray job was not found. Submit a Ray job via an HTTP request.", "JobId", rayJobInstance.Status.JobId)
				if _, err := rayDashboardClient.SubmitJob(ctx, rayJobInstance); err != nil {
					r.Log.Error(err, "Failed to submit the Ray job", "JobId", rayJobInstance.Status.JobId)
					return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
				}
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
			}
			r.Log.Error(err, "Failed to get job info", "JobId", rayJobInstance.Status.JobId)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		r.Log.Info("GetJobInfo", "Job Info", jobInfo)

		// If the JobStatus is in a terminal status, such as SUCCEEDED, FAILED, or STOPPED, it is impossible for the Ray job
		// to transition to any other. Additionally, RayJob does not currently support retries. Hence, we can mark the RayJob
		// as "Complete" to avoid unnecessary reconciliation.
		jobDeploymentStatus := rayv1.JobDeploymentStatusRunning
		if rayv1.IsJobTerminal(jobInfo.JobStatus) {
			jobDeploymentStatus = rayv1.JobDeploymentStatusComplete
		}
		// Always update RayClusterStatus along with JobStatus and JobDeploymentStatus updates.
		rayJobInstance.Status.RayClusterStatus = rayClusterInstance.Status
		rayJobInstance.Status.JobStatus = jobInfo.JobStatus
		rayJobInstance.Status.Message = jobInfo.Message
		rayJobInstance.Status.StartTime = utils.ConvertUnixTimeToMetav1Time(jobInfo.StartTime)
		rayJobInstance.Status.JobDeploymentStatus = jobDeploymentStatus
	case rayv1.JobDeploymentStatusSuspending:
		// The `suspend` operation should be atomic. In other words, if users set the `suspend` flag to true and then immediately
		// set it back to false, either all of the RayJob's associated resources should be cleaned up, or no resources should be
		// cleaned up at all. To keep the atomicity, if a RayJob is in the `Suspending` status, we should delete all of its
		// associated resources and then transition the status to `Suspended` no matter the value of the `suspend` flag.

		// TODO (kevin85421): Currently, Ray doesn't have a best practice to stop a Ray job gracefully. At this moment,
		// KubeRay doesn't stop the Ray job before suspending the RayJob. If users want to stop the Ray job by SIGTERM,
		// users need to set the Pod's preStop hook by themselves.
		isClusterDeleted, err := r.deleteClusterResources(ctx, rayJobInstance)
		if err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		isJobDeleted, err := r.deleteSubmitterJob(ctx, rayJobInstance)
		if err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		if !isClusterDeleted || !isJobDeleted {
			r.Log.Info("The release of the compute resources has not been completed yet. " +
				"Wait for the resources to be deleted before the status transitions to avoid a resource leak.")
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
		}

		// Reset the RayCluster and Ray job related status.
		rayJobInstance.Status.RayClusterStatus = rayv1.RayClusterStatus{}
		rayJobInstance.Status.RayClusterName = ""
		rayJobInstance.Status.DashboardURL = ""
		rayJobInstance.Status.JobId = ""
		rayJobInstance.Status.Message = ""
		// Reset the JobStatus to JobStatusNew and transition the JobDeploymentStatus to `Suspended`.
		rayJobInstance.Status.JobStatus = rayv1.JobStatusNew
		rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusSuspended
	case rayv1.JobDeploymentStatusSuspended:
		if !rayJobInstance.Spec.Suspend {
			r.Log.Info("The status is 'Suspended', but the suspend flag is false. Transition the status to 'New'.")
			rayJobInstance.Status.JobStatus = rayv1.JobStatusNew
			rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusNew
			break
		}
		// TODO (kevin85421): We may not need to requeue the RayJob if it has already been suspended.
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	case rayv1.JobDeploymentStatusComplete:
		// If this RayJob uses an existing RayCluster (i.e., ClusterSelector is set), we should not delete the RayCluster.
		r.Log.Info("JobDeploymentStatusComplete", "RayJob", rayJobInstance.Name, "ShutdownAfterJobFinishes", rayJobInstance.Spec.ShutdownAfterJobFinishes, "ClusterSelector", rayJobInstance.Spec.ClusterSelector)
		if rayJobInstance.Spec.ShutdownAfterJobFinishes && len(rayJobInstance.Spec.ClusterSelector) == 0 {
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
				// We only need to delete the RayCluster. We don't need to delete the submitter Kubernetes Job so that users can still access
				// the driver logs. In addition, a completed Kubernetes Job does not actually use any compute resources.
				if _, err = r.deleteClusterResources(ctx, rayJobInstance); err != nil {
					return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
				}
			}
		}
		// If the RayJob is completed, we should not requeue it.
		return ctrl.Result{}, nil
	default:
		r.Log.Info("Unknown JobDeploymentStatus", "JobDeploymentStatus", rayJobInstance.Status.JobDeploymentStatus)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	}

	// This is the only place where we update the RayJob status. Please do NOT add any code
	// between the above switch statement and the following code.
	if err = r.updateRayJobStatus(ctx, originalRayJobInstance, rayJobInstance); err != nil {
		r.Log.Info("Failed to update RayJob status", "error", err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}
	return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
}

// createK8sJobIfNeed creates a Kubernetes Job for the RayJob if it doesn't exist.
func (r *RayJobReconciler) createK8sJobIfNeed(ctx context.Context, rayJobInstance *rayv1.RayJob, rayClusterInstance *rayv1.RayCluster) error {
	job := &batchv1.Job{}
	namespacedName := getK8sJobNamespacedName(rayJobInstance)
	if err := r.Client.Get(ctx, namespacedName, job); err != nil {
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

	// Users can use `RAY_DASHBOARD_ADDRESS` to specify the dashboard address and `RAY_JOB_SUBMISSION_ID` to specify the job id to avoid
	// double submission in the `ray job submit` command. For example:
	// ray job submit --address=http://$RAY_DASHBOARD_ADDRESS --submission-id=$RAY_JOB_SUBMISSION_ID ...
	submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env = append(submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env, corev1.EnvVar{
		Name:  utils.RAY_DASHBOARD_ADDRESS,
		Value: rayJobInstance.Status.DashboardURL,
	})
	submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env = append(submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env, corev1.EnvVar{
		Name:  utils.RAY_JOB_SUBMISSION_ID,
		Value: rayJobInstance.Status.JobId,
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
				utils.RayOriginatedFromCRNameLabelKey: rayJobInstance.Name,
				utils.RayOriginatedFromCRDLabelKey:    utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD),
				utils.KubernetesCreatedByLabelKey:     utils.ComponentName,
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

// deleteSubmitterJob deletes the submitter Job associated with the RayJob.
func (r *RayJobReconciler) deleteSubmitterJob(ctx context.Context, rayJobInstance *rayv1.RayJob) (bool, error) {
	if rayJobInstance.Spec.SubmissionMode == rayv1.HTTPMode {
		return true, nil
	}
	var isJobDeleted bool

	// Since the name of the Kubernetes Job is the same as the RayJob, we need to delete the Kubernetes Job
	// and its Pods when suspending. A new submitter Kubernetes Job must be created to resubmit the
	// Ray job if the RayJob is resumed.
	job := &batchv1.Job{}
	namespacedName := getK8sJobNamespacedName(rayJobInstance)
	if err := r.Client.Get(ctx, namespacedName, job); err != nil {
		if errors.IsNotFound(err) {
			isJobDeleted = true
			r.Log.Info("The submitter Kubernetes Job has been already deleted", "RayJob", rayJobInstance.Name, "Kubernetes Job", job.Name)
		} else {
			r.Log.Error(err, "Failed to get Kubernetes Job")
			return false, err
		}
	} else {
		if !job.DeletionTimestamp.IsZero() {
			r.Log.Info("The Job deletion is ongoing.", "RayJob", rayJobInstance.Name, "Submitter K8s Job", job.Name)
		} else {
			if err := r.Client.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
				return false, err
			}
			r.Log.Info("The associated submitter Job is deleted", "RayJob", rayJobInstance.Name, "Submitter K8s Job", job.Name)
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Deleted", "Deleted submitter K8s Job %s", job.Name)
		}
	}

	r.Log.Info("deleteSubmitterJob", "isJobDeleted", isJobDeleted)
	return isJobDeleted, nil
}

// deleteClusterResources deletes the RayCluster associated with the RayJob to release the compute resources.
func (r *RayJobReconciler) deleteClusterResources(ctx context.Context, rayJobInstance *rayv1.RayJob) (bool, error) {
	namespace := rayJobInstance.Namespace
	clusterIdentifier := types.NamespacedName{
		Name:      rayJobInstance.Status.RayClusterName,
		Namespace: namespace,
	}

	var isClusterDeleted bool
	cluster := rayv1.RayCluster{}
	if err := r.Get(ctx, clusterIdentifier, &cluster); err != nil {
		if errors.IsNotFound(err) {
			// If the cluster is not found, it means the cluster has been already deleted.
			// Don't return error to make this function idempotent.
			isClusterDeleted = true
			r.Log.Info("The associated cluster has been already deleted and it can not be found", "RayCluster", clusterIdentifier)
		} else {
			return false, err
		}
	} else {
		if !cluster.DeletionTimestamp.IsZero() {
			r.Log.Info("The cluster deletion is ongoing.", "rayjob", rayJobInstance.Name, "raycluster", cluster.Name)
		} else {
			if err := r.Delete(ctx, &cluster); err != nil {
				return false, err
			}
			r.Log.Info("The associated cluster is deleted", "RayCluster", clusterIdentifier)
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Deleted", "Deleted cluster %s", rayJobInstance.Status.RayClusterName)
		}
	}

	r.Log.Info("deleteClusterResources", "isClusterDeleted", isClusterDeleted)
	return isClusterDeleted, nil
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
	rayJob.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusInitializing
	return nil
}

func (r *RayJobReconciler) updateRayJobStatus(ctx context.Context, oldRayJob *rayv1.RayJob, newRayJob *rayv1.RayJob) error {
	oldRayJobStatus := oldRayJob.Status
	newRayJobStatus := newRayJob.Status
	r.Log.Info("updateRayJobStatus", "oldRayJobStatus", oldRayJobStatus, "newRayJobStatus", newRayJobStatus)
	// If a status field is crucial for the RayJob state machine, it MUST be
	// updated with a distinct JobStatus or JobDeploymentStatus value.
	if oldRayJobStatus.JobStatus != newRayJobStatus.JobStatus ||
		oldRayJobStatus.JobDeploymentStatus != newRayJobStatus.JobDeploymentStatus {

		if newRayJobStatus.JobDeploymentStatus == rayv1.JobDeploymentStatusComplete {
			newRayJob.Status.EndTime = &metav1.Time{Time: time.Now()}
		}

		r.Log.Info("updateRayJobStatus", "old JobStatus", oldRayJobStatus.JobStatus, "new JobStatus", newRayJobStatus.JobStatus,
			"old JobDeploymentStatus", oldRayJobStatus.JobDeploymentStatus, "new JobDeploymentStatus", newRayJobStatus.JobDeploymentStatus)
		if err := r.Status().Update(ctx, newRayJob); err != nil {
			return err
		}
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
			if len(rayJobInstance.Spec.ClusterSelector) != 0 {
				err := fmt.Errorf("we have choosed the cluster selector mode, failed to find the cluster named %v, err: %v", rayClusterInstanceName, err)
				return nil, err
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
	labels := make(map[string]string, len(rayJobInstance.Labels))
	for key, value := range rayJobInstance.Labels {
		labels[key] = value
	}
	labels[utils.RayOriginatedFromCRNameLabelKey] = rayJobInstance.Name
	labels[utils.RayOriginatedFromCRDLabelKey] = utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD)
	rayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
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

func (r *RayJobReconciler) updateStatusToSuspendingIfNeeded(ctx context.Context, rayJob *rayv1.RayJob) bool {
	if !rayJob.Spec.Suspend {
		return false
	}
	// In KubeRay, only `Running` and `Initializing` are allowed to transition to `Suspending`.
	validTransitions := map[rayv1.JobDeploymentStatus]struct{}{
		rayv1.JobDeploymentStatusRunning:      {},
		rayv1.JobDeploymentStatusInitializing: {},
	}
	if _, ok := validTransitions[rayJob.Status.JobDeploymentStatus]; !ok {
		r.Log.Info("The current status is not allowed to transition to `Suspending`", "RayJob", rayJob.Name, "JobDeploymentStatus", rayJob.Status.JobDeploymentStatus)
		return false
	}
	r.Log.Info(fmt.Sprintf("Try to transition the status from `%s` to `Suspending`", rayJob.Status.JobDeploymentStatus), "RayJob", rayJob.Name)
	rayJob.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusSuspending
	return true
}

func (r *RayJobReconciler) checkK8sJobAndUpdateStatusIfNeeded(ctx context.Context, rayJob *rayv1.RayJob, job *batchv1.Job) bool {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			r.Log.Info("The submitter Kubernetes Job has failed. Attempting to transition the status to `Complete`.", "RayJob", rayJob.Name, "Submitter K8s Job", job.Name, "Reason", cond.Reason, "Message", cond.Message)
			rayJob.Status.Message = "The submitter Kubernetes Job is failed. Reason: " + cond.Reason + ". Message: " + cond.Message
			rayJob.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusComplete
			return true
		}
	}
	return false
}

func validateRayJobSpec(rayJob *rayv1.RayJob) error {
	// KubeRay has some limitations for the suspend operation. The limitations are a subset of the limitations of
	// Kueue (https://kueue.sigs.k8s.io/docs/tasks/run_rayjobs/#c-limitations). For example, KubeRay allows users
	// to suspend a RayJob with autoscaling enabled, but Kueue doesn't.
	if rayJob.Spec.Suspend && !rayJob.Spec.ShutdownAfterJobFinishes {
		return fmt.Errorf("a RayJob with shutdownAfterJobFinishes set to false is not allowed to be suspended")
	}
	if rayJob.Spec.Suspend && len(rayJob.Spec.ClusterSelector) != 0 {
		return fmt.Errorf("the ClusterSelector mode doesn't support the suspend operation")
	}
	if rayJob.Spec.RayClusterSpec == nil && len(rayJob.Spec.ClusterSelector) == 0 {
		return fmt.Errorf("one of RayClusterSpec or ClusterSelector must be set")
	}
	// Validate whether RuntimeEnvYAML is a valid YAML string. Note that this only checks its validity
	// as a YAML string, not its adherence to the runtime environment schema.
	if _, err := utils.UnmarshalRuntimeEnvYAML(rayJob.Spec.RuntimeEnvYAML); err != nil {
		return err
	}
	return nil
}

// getK8sJobNamespacedName is the only place to associate the RayJob with the submitter Kubernetes Job.
func getK8sJobNamespacedName(rayJob *rayv1.RayJob) types.NamespacedName {
	return types.NamespacedName{
		Namespace: rayJob.Namespace,
		Name:      rayJob.Name,
	}
}
