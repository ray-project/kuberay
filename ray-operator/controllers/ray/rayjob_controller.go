package ray

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
	Recorder record.EventRecorder

	dashboardClientFunc func() utils.RayDashboardClientInterface
}

// NewRayJobReconciler returns a new reconcile.Reconciler
func NewRayJobReconciler(_ context.Context, mgr manager.Manager, provider utils.ClientProvider) *RayJobReconciler {
	dashboardClientFunc := provider.GetDashboardClient(mgr)
	return &RayJobReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
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
// +kubebuilder:rbac:groups=core,resources=services/proxy,verbs=get;update;patch;create
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
	logger := ctrl.LoggerFrom(ctx)

	// Get RayJob instance
	var err error
	rayJobInstance := &rayv1.RayJob{}
	if err := r.Get(ctx, request.NamespacedName, rayJobInstance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request. Stop reconciliation.
			logger.Info("RayJob resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get RayJob")
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	if manager := utils.ManagedByExternalController(rayJobInstance.Spec.ManagedBy); manager != nil {
		logger.Info("Skipping RayJob managed by a custom controller", "managed-by", manager)
		return ctrl.Result{}, nil
	}

	if !rayJobInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("RayJob is being deleted", "DeletionTimestamp", rayJobInstance.ObjectMeta.DeletionTimestamp)
		// If the JobStatus is not terminal, it is possible that the Ray job is still running. This includes
		// the case where JobStatus is JobStatusNew.
		if !rayv1.IsJobTerminal(rayJobInstance.Status.JobStatus) {
			rayClusterNamespacedName := common.RayJobRayClusterNamespacedName(rayJobInstance)
			rayClusterInstance := &rayv1.RayCluster{}
			if err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance); err != nil {
				logger.Error(err, "Failed to get RayCluster")
			}

			rayDashboardClient := r.dashboardClientFunc()
			if err := rayDashboardClient.InitClient(ctx, rayJobInstance.Status.DashboardURL, rayClusterInstance); err != nil {
				logger.Error(err, "Failed to initialize dashboard client")
			}
			if err := rayDashboardClient.StopJob(ctx, rayJobInstance.Status.JobId); err != nil {
				logger.Error(err, "Failed to stop job for RayJob")
			}
		}

		logger.Info("Remove the finalizer no matter StopJob() succeeds or not.", "finalizer", utils.RayJobStopJobFinalizer)
		controllerutil.RemoveFinalizer(rayJobInstance, utils.RayJobStopJobFinalizer)
		if err := r.Update(ctx, rayJobInstance); err != nil {
			logger.Error(err, "Failed to remove finalizer for RayJob")
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	if err := utils.ValidateRayJobSpec(rayJobInstance); err != nil {
		logger.Error(err, "The RayJob spec is invalid")
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeWarning, string(utils.InvalidRayJobSpec),
			"The RayJob spec is invalid %s/%s: %v", rayJobInstance.Namespace, rayJobInstance.Name, err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	if err := utils.ValidateRayJobStatus(rayJobInstance); err != nil {
		logger.Error(err, "The RayJob status is invalid")
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeWarning, string(utils.InvalidRayJobStatus),
			"The RayJob status is invalid %s/%s: %v", rayJobInstance.Namespace, rayJobInstance.Name, err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Please do NOT modify `originalRayJobInstance` in the following code.
	originalRayJobInstance := rayJobInstance.DeepCopy()

	logger.Info("RayJob", "JobStatus", rayJobInstance.Status.JobStatus, "JobDeploymentStatus", rayJobInstance.Status.JobDeploymentStatus, "SubmissionMode", rayJobInstance.Spec.SubmissionMode)
	switch rayJobInstance.Status.JobDeploymentStatus {
	case rayv1.JobDeploymentStatusNew:
		if !controllerutil.ContainsFinalizer(rayJobInstance, utils.RayJobStopJobFinalizer) {
			logger.Info("Add a finalizer", "finalizer", utils.RayJobStopJobFinalizer)
			controllerutil.AddFinalizer(rayJobInstance, utils.RayJobStopJobFinalizer)
			if err := r.Update(ctx, rayJobInstance); err != nil {
				logger.Error(err, "Failed to update RayJob with finalizer")
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
		}
		// Set `Status.JobDeploymentStatus` to `JobDeploymentStatusInitializing`, and initialize `Status.JobId`
		// and `Status.RayClusterName` prior to avoid duplicate job submissions and cluster creations.
		logger.Info("JobDeploymentStatusNew")
		if err = initRayJobStatusIfNeed(ctx, rayJobInstance); err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
	case rayv1.JobDeploymentStatusInitializing:
		if shouldUpdate := updateStatusToSuspendingIfNeeded(ctx, rayJobInstance); shouldUpdate {
			break
		}

		if shouldUpdate := checkActiveDeadlineAndUpdateStatusIfNeeded(ctx, rayJobInstance); shouldUpdate {
			break
		}

		var rayClusterInstance *rayv1.RayCluster
		if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayJobInstance); err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}

		// Check the current status of RayCluster before submitting.
		if clientURL := rayJobInstance.Status.DashboardURL; clientURL == "" {
			if rayClusterInstance.Status.State != rayv1.Ready { //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
				logger.Info("Wait for the RayCluster.Status.State to be ready before submitting the job.", "RayCluster", rayClusterInstance.Name, "State", rayClusterInstance.Status.State) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}

			if clientURL, err = utils.FetchHeadServiceURL(ctx, r.Client, rayClusterInstance, utils.DashboardPortName); err != nil || clientURL == "" {
				logger.Error(err, "Failed to get the dashboard URL after the RayCluster is ready!", "RayCluster", rayClusterInstance.Name)
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			rayJobInstance.Status.DashboardURL = clientURL
		}

		if rayJobInstance.Spec.SubmissionMode == rayv1.InteractiveMode {
			logger.Info("SubmissionMode is InteractiveMode and the RayCluster is created. Transition the status from `Initializing` to `Waiting`.")
			rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusWaiting
			break
		}

		if rayJobInstance.Spec.SubmissionMode == rayv1.K8sJobMode {
			if err := r.createK8sJobIfNeed(ctx, rayJobInstance, rayClusterInstance); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
		}

		logger.Info("Both RayCluster and the submitter K8s Job are created. Transition the status from `Initializing` to `Running`.", "SubmissionMode", rayJobInstance.Spec.SubmissionMode,
			"RayCluster", rayJobInstance.Status.RayClusterName)
		rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusRunning
	case rayv1.JobDeploymentStatusWaiting:
		// Try to get the Ray job id from rayJob.Spec.JobId
		if rayJobInstance.Spec.JobId == "" {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
		}

		rayJobInstance.Status.JobId = rayJobInstance.Spec.JobId
		rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusRunning
	case rayv1.JobDeploymentStatusRunning:
		if shouldUpdate := updateStatusToSuspendingIfNeeded(ctx, rayJobInstance); shouldUpdate {
			break
		}

		if shouldUpdate := checkActiveDeadlineAndUpdateStatusIfNeeded(ctx, rayJobInstance); shouldUpdate {
			break
		}

		job := &batchv1.Job{}
		if rayJobInstance.Spec.SubmissionMode == rayv1.K8sJobMode {
			// If the submitting Kubernetes Job reaches the backoff limit, transition the status to `Complete` or `Failed`.
			// This is because, beyond this point, it becomes impossible for the submitter to submit any further Ray jobs.
			// For light-weight mode, we don't transition the status to `Complete` or `Failed` based on the number of failed
			// requests. Instead, users can use the `ActiveDeadlineSeconds` to ensure that the RayJob in the light-weight
			// mode is not stuck in the `Running` status indefinitely.
			namespacedName := common.RayJobK8sJobNamespacedName(rayJobInstance)
			if err := r.Client.Get(ctx, namespacedName, job); err != nil {
				logger.Error(err, "Failed to get the submitter Kubernetes Job for RayJob", "NamespacedName", namespacedName)
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			if shouldUpdate := checkK8sJobAndUpdateStatusIfNeeded(ctx, rayJobInstance, job); shouldUpdate {
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
		if err := rayDashboardClient.InitClient(ctx, rayJobInstance.Status.DashboardURL, rayClusterInstance); err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}

		jobInfo, err := rayDashboardClient.GetJobInfo(ctx, rayJobInstance.Status.JobId)
		if err != nil {
			// If the Ray job was not found, GetJobInfo returns a BadRequest error.
			if rayJobInstance.Spec.SubmissionMode == rayv1.HTTPMode && errors.IsBadRequest(err) {
				logger.Info("The Ray job was not found. Submit a Ray job via an HTTP request.", "JobId", rayJobInstance.Status.JobId)
				if _, err := rayDashboardClient.SubmitJob(ctx, rayJobInstance); err != nil {
					logger.Error(err, "Failed to submit the Ray job", "JobId", rayJobInstance.Status.JobId)
					return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
				}
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
			}
			logger.Error(err, "Failed to get job info", "JobId", rayJobInstance.Status.JobId)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		logger.Info("GetJobInfo", "Job Info", jobInfo)

		// If the JobStatus is in a terminal status, such as SUCCEEDED, FAILED, or STOPPED, it is impossible for the Ray job
		// to transition to any other. Additionally, RayJob does not currently support retries. Hence, we can mark the RayJob
		// as "Complete" or "Failed" to avoid unnecessary reconciliation.
		jobDeploymentStatus := rayv1.JobDeploymentStatusRunning
		reason := rayv1.JobFailedReason("")
		isJobTerminal := rayv1.IsJobTerminal(jobInfo.JobStatus)
		// If in K8sJobMode, further refine the terminal condition by checking if the submitter Job has finished.
		// See https://github.com/ray-project/kuberay/pull/1919 for reasons.
		if rayJobInstance.Spec.SubmissionMode == rayv1.K8sJobMode {
			_, finished := utils.IsJobFinished(job)
			isJobTerminal = isJobTerminal && finished
		}

		if isJobTerminal {
			jobDeploymentStatus = rayv1.JobDeploymentStatusComplete
			if jobInfo.JobStatus == rayv1.JobStatusFailed {
				jobDeploymentStatus = rayv1.JobDeploymentStatusFailed
				reason = rayv1.AppFailed
			}
		}

		// Always update RayClusterStatus along with JobStatus and JobDeploymentStatus updates.
		rayJobInstance.Status.RayClusterStatus = rayClusterInstance.Status
		rayJobInstance.Status.JobStatus = jobInfo.JobStatus
		rayJobInstance.Status.JobDeploymentStatus = jobDeploymentStatus
		rayJobInstance.Status.Reason = reason
		rayJobInstance.Status.Message = jobInfo.Message
	case rayv1.JobDeploymentStatusSuspending, rayv1.JobDeploymentStatusRetrying:
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
			logger.Info("The release of the compute resources has not been completed yet. " +
				"Wait for the resources to be deleted before the status transitions to avoid a resource leak.")
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
		}

		// Reset the RayCluster and Ray job related status.
		rayJobInstance.Status.RayClusterStatus = rayv1.RayClusterStatus{}
		rayJobInstance.Status.RayClusterName = ""
		rayJobInstance.Status.DashboardURL = ""
		rayJobInstance.Status.JobId = ""
		rayJobInstance.Status.Message = ""
		rayJobInstance.Status.Reason = ""
		// Reset the JobStatus to JobStatusNew and transition the JobDeploymentStatus to `Suspended`.
		rayJobInstance.Status.JobStatus = rayv1.JobStatusNew

		if rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusSuspending {
			rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusSuspended
		}
		if rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusRetrying {
			rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusNew
		}
	case rayv1.JobDeploymentStatusSuspended:
		if !rayJobInstance.Spec.Suspend {
			logger.Info("The status is 'Suspended', but the suspend flag is false. Transition the status to 'New'.")
			rayJobInstance.Status.JobStatus = rayv1.JobStatusNew
			rayJobInstance.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusNew
			break
		}
		// TODO (kevin85421): We may not need to requeue the RayJob if it has already been suspended.
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	case rayv1.JobDeploymentStatusComplete, rayv1.JobDeploymentStatusFailed:
		// If this RayJob uses an existing RayCluster (i.e., ClusterSelector is set), we should not delete the RayCluster.
		ttlSeconds := rayJobInstance.Spec.TTLSecondsAfterFinished
		nowTime := time.Now()
		shutdownTime := rayJobInstance.Status.EndTime.Add(time.Duration(ttlSeconds) * time.Second)
		logger.Info(string(rayJobInstance.Status.JobDeploymentStatus),
			"ShutdownAfterJobFinishes", rayJobInstance.Spec.ShutdownAfterJobFinishes,
			"ClusterSelector", rayJobInstance.Spec.ClusterSelector,
			"ttlSecondsAfterFinished", ttlSeconds,
			"Status.endTime", rayJobInstance.Status.EndTime,
			"Now", nowTime,
			"ShutdownTime", shutdownTime)

		if features.Enabled(features.RayJobDeletionPolicy) &&
			rayJobInstance.Spec.DeletionPolicy != nil &&
			*rayJobInstance.Spec.DeletionPolicy != rayv1.DeleteNoneDeletionPolicy &&
			len(rayJobInstance.Spec.ClusterSelector) == 0 {
			logger.Info("Shutdown behavior is defined by the deletion policy", "deletionPolicy", rayJobInstance.Spec.DeletionPolicy)
			if shutdownTime.After(nowTime) {
				delta := int32(time.Until(shutdownTime.Add(2 * time.Second)).Seconds())
				logger.Info("shutdownTime not reached, requeue this RayJob for n seconds", "seconds", delta)
				return ctrl.Result{RequeueAfter: time.Duration(delta) * time.Second}, nil
			}

			switch *rayJobInstance.Spec.DeletionPolicy {
			case rayv1.DeleteClusterDeletionPolicy:
				logger.Info("Deleting RayCluster", "RayCluster", rayJobInstance.Status.RayClusterName)
				_, err = r.deleteClusterResources(ctx, rayJobInstance)
			case rayv1.DeleteWorkersDeletionPolicy:
				logger.Info("Suspending all worker groups", "RayCluster", rayJobInstance.Status.RayClusterName)
				err = r.suspendWorkerGroups(ctx, rayJobInstance)
			case rayv1.DeleteSelfDeletionPolicy:
				logger.Info("Deleting RayJob")
				err = r.Client.Delete(ctx, rayJobInstance)
			default:
			}
			if err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
		}

		if (!features.Enabled(features.RayJobDeletionPolicy) || rayJobInstance.Spec.DeletionPolicy == nil) && rayJobInstance.Spec.ShutdownAfterJobFinishes && len(rayJobInstance.Spec.ClusterSelector) == 0 {
			logger.Info("Shutdown behavior is defined by the `ShutdownAfterJobFinishes` flag", "shutdownAfterJobFinishes", rayJobInstance.Spec.ShutdownAfterJobFinishes)
			if shutdownTime.After(nowTime) {
				delta := int32(time.Until(shutdownTime.Add(2 * time.Second)).Seconds())
				logger.Info("shutdownTime not reached, requeue this RayJob for n seconds", "seconds", delta)
				return ctrl.Result{RequeueAfter: time.Duration(delta) * time.Second}, nil
			}
			if s := os.Getenv(utils.DELETE_RAYJOB_CR_AFTER_JOB_FINISHES); strings.ToLower(s) == "true" {
				err = r.Client.Delete(ctx, rayJobInstance)
				logger.Info("RayJob is deleted")
			} else {
				// We only need to delete the RayCluster. We don't need to delete the submitter Kubernetes Job so that users can still access
				// the driver logs. In addition, a completed Kubernetes Job does not actually use any compute resources.
				_, err = r.deleteClusterResources(ctx, rayJobInstance)
				logger.Info("RayCluster is deleted", "RayCluster", rayJobInstance.Status.RayClusterName)
			}
			if err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
		}

		// If the RayJob is completed, we should not requeue it.
		return ctrl.Result{}, nil
	default:
		logger.Info("Unknown JobDeploymentStatus", "JobDeploymentStatus", rayJobInstance.Status.JobDeploymentStatus)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	}
	checkBackoffLimitAndUpdateStatusIfNeeded(ctx, rayJobInstance)

	// This is the only place where we update the RayJob status. Please do NOT add any code
	// between `checkBackoffLimitAndUpdateStatusIfNeeded` and the following code.
	if err = r.updateRayJobStatus(ctx, originalRayJobInstance, rayJobInstance); err != nil {
		logger.Info("Failed to update RayJob status", "error", err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}
	return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
}

// checkBackoffLimitAndUpdateStatusIfNeeded determines if a RayJob is eligible for retry based on the configured backoff limit,
// the job's success status, and its failure status. If eligible, sets the JobDeploymentStatus to Retrying.
func checkBackoffLimitAndUpdateStatusIfNeeded(ctx context.Context, rayJob *rayv1.RayJob) {
	logger := ctrl.LoggerFrom(ctx)

	failedCount := int32(0)
	if rayJob.Status.Failed != nil {
		failedCount = *rayJob.Status.Failed
	}

	succeededCount := int32(0)
	if rayJob.Status.Succeeded != nil {
		succeededCount = *rayJob.Status.Succeeded
	}

	if rayJob.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusFailed {
		failedCount++
	}

	if rayJob.Status.JobStatus == rayv1.JobStatusSucceeded && rayJob.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusComplete {
		succeededCount++
	}

	rayJob.Status.Failed = ptr.To[int32](failedCount)
	rayJob.Status.Succeeded = ptr.To[int32](succeededCount)

	if rayJob.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusFailed && rayJob.Spec.BackoffLimit != nil && *rayJob.Status.Failed < *rayJob.Spec.BackoffLimit+1 {
		if rayJob.Status.Reason == rayv1.DeadlineExceeded {
			logger.Info(
				"RayJob is not eligible for retry due to failure with DeadlineExceeded",
				"backoffLimit", *rayJob.Spec.BackoffLimit,
				"succeeded", *rayJob.Status.Succeeded,
				"failed", *rayJob.Status.Failed,
			)
			return
		}
		logger.Info("RayJob is eligible for retry, setting JobDeploymentStatus to Retrying",
			"backoffLimit", *rayJob.Spec.BackoffLimit, "succeeded", *rayJob.Status.Succeeded, "failed", *rayJob.Status.Failed)
		rayJob.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusRetrying
	}
}

// createK8sJobIfNeed creates a Kubernetes Job for the RayJob if it doesn't exist.
func (r *RayJobReconciler) createK8sJobIfNeed(ctx context.Context, rayJobInstance *rayv1.RayJob, rayClusterInstance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	job := &batchv1.Job{}
	namespacedName := common.RayJobK8sJobNamespacedName(rayJobInstance)
	if err := r.Client.Get(ctx, namespacedName, job); err != nil {
		if errors.IsNotFound(err) {
			submitterTemplate, err := getSubmitterTemplate(ctx, rayJobInstance, rayClusterInstance)
			if err != nil {
				return err
			}
			return r.createNewK8sJob(ctx, rayJobInstance, submitterTemplate)
		}
		return err
	}

	logger.Info("The submitter Kubernetes Job for RayJob already exists", "Kubernetes Job", job.Name)
	return nil
}

// getSubmitterTemplate builds the submitter pod template for the Ray job.
func getSubmitterTemplate(ctx context.Context, rayJobInstance *rayv1.RayJob, rayClusterInstance *rayv1.RayCluster) (corev1.PodTemplateSpec, error) {
	logger := ctrl.LoggerFrom(ctx)
	var submitterTemplate corev1.PodTemplateSpec

	// Set the default value for the optional field SubmitterPodTemplate if not provided.
	if rayJobInstance.Spec.SubmitterPodTemplate == nil {
		submitterTemplate = common.GetDefaultSubmitterTemplate(rayClusterInstance)
		logger.Info("default submitter template is used")
	} else {
		submitterTemplate = *rayJobInstance.Spec.SubmitterPodTemplate.DeepCopy()
		logger.Info("user-provided submitter template is used; the first container is assumed to be the submitter")
	}

	// If the command in the submitter pod template isn't set, use the default command.
	if len(submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command) == 0 {
		k8sJobCommand, err := common.GetK8sJobCommand(rayJobInstance)
		if err != nil {
			return corev1.PodTemplateSpec{}, err
		}
		submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command = []string{"/bin/bash"}
		submitterTemplate.Spec.Containers[utils.RayContainerIndex].Args = []string{"-c", strings.Join(k8sJobCommand, " ")}
		logger.Info("No command is specified in the user-provided template. Default command is used", "command", k8sJobCommand)
	} else {
		logger.Info("User-provided command is used", "command", submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command)
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
	logger := ctrl.LoggerFrom(ctx)
	submitterBackoffLimit := ptr.To[int32](2)
	if rayJobInstance.Spec.SubmitterConfig != nil && rayJobInstance.Spec.SubmitterConfig.BackoffLimit != nil {
		submitterBackoffLimit = rayJobInstance.Spec.SubmitterConfig.BackoffLimit
	}
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
			BackoffLimit: submitterBackoffLimit,
			Template:     submitterTemplate,
		},
	}

	// Set the ownership in order to do the garbage collection by k8s.
	if err := ctrl.SetControllerReference(rayJobInstance, job, r.Scheme); err != nil {
		return err
	}

	// Create the Kubernetes Job
	if err := r.Client.Create(ctx, job); err != nil {
		logger.Error(err, "Failed to create new submitter Kubernetes Job for RayJob")
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeWarning, string(utils.FailedToCreateRayJobSubmitter), "Failed to create new Kubernetes Job %s/%s: %v", job.Namespace, job.Name, err)
		return err
	}
	logger.Info("Created submitter Kubernetes Job for RayJob", "Kubernetes Job", job.Name)
	r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, string(utils.CreatedRayJobSubmitter), "Created Kubernetes Job %s/%s", job.Namespace, job.Name)
	return nil
}

// deleteSubmitterJob deletes the submitter Job associated with the RayJob.
func (r *RayJobReconciler) deleteSubmitterJob(ctx context.Context, rayJobInstance *rayv1.RayJob) (bool, error) {
	logger := ctrl.LoggerFrom(ctx)
	if rayJobInstance.Spec.SubmissionMode == rayv1.HTTPMode {
		return true, nil
	}
	var isJobDeleted bool

	// Since the name of the Kubernetes Job is the same as the RayJob, we need to delete the Kubernetes Job
	// and its Pods when suspending. A new submitter Kubernetes Job must be created to resubmit the
	// Ray job if the RayJob is resumed.
	job := &batchv1.Job{}
	namespacedName := common.RayJobK8sJobNamespacedName(rayJobInstance)
	if err := r.Client.Get(ctx, namespacedName, job); err != nil {
		if errors.IsNotFound(err) {
			isJobDeleted = true
			logger.Info("The submitter Kubernetes Job has been already deleted", "Kubernetes Job", job.Name)
		} else {
			return false, err
		}
	} else {
		if !job.DeletionTimestamp.IsZero() {
			logger.Info("The deletion of submitter Kubernetes Job for RayJob is ongoing.", "Submitter K8s Job", job.Name)
		} else {
			if err := r.Client.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
				r.Recorder.Eventf(rayJobInstance, corev1.EventTypeWarning, string(utils.FailedToDeleteRayJobSubmitter), "Failed to delete submitter K8s Job %s/%s: %v", job.Namespace, job.Name, err)
				return false, err
			}
			logger.Info("The associated submitter Kubernetes Job for RayJob is deleted", "Submitter K8s Job", job.Name)
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, string(utils.DeletedRayJobSubmitter), "Deleted submitter K8s Job %s/%s", job.Namespace, job.Name)
		}
	}

	logger.Info("deleteSubmitterJob", "isJobDeleted", isJobDeleted)
	return isJobDeleted, nil
}

// deleteClusterResources deletes the RayCluster associated with the RayJob to release the compute resources.
func (r *RayJobReconciler) deleteClusterResources(ctx context.Context, rayJobInstance *rayv1.RayJob) (bool, error) {
	logger := ctrl.LoggerFrom(ctx)
	clusterIdentifier := common.RayJobRayClusterNamespacedName(rayJobInstance)

	var isClusterDeleted bool
	cluster := rayv1.RayCluster{}
	if err := r.Get(ctx, clusterIdentifier, &cluster); err != nil {
		if errors.IsNotFound(err) {
			// If the cluster is not found, it means the cluster has been already deleted.
			// Don't return error to make this function idempotent.
			isClusterDeleted = true
			logger.Info("The associated RayCluster for RayJob has been already deleted and it can not be found", "RayCluster", clusterIdentifier)
		} else {
			return false, err
		}
	} else {
		if !cluster.DeletionTimestamp.IsZero() {
			logger.Info("The deletion of the associated RayCluster for RayJob is ongoing.", "RayCluster", cluster.Name)
		} else {
			if err := r.Delete(ctx, &cluster); err != nil {
				r.Recorder.Eventf(rayJobInstance, corev1.EventTypeWarning, string(utils.FailedToDeleteRayCluster), "Failed to delete cluster %s/%s: %v", cluster.Namespace, cluster.Name, err)
				return false, err
			}
			logger.Info("The associated RayCluster for RayJob is deleted", "RayCluster", clusterIdentifier)
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, string(utils.DeletedRayCluster), "Deleted cluster %s/%s", cluster.Namespace, cluster.Name)
		}
	}

	logger.Info("deleteClusterResources", "isClusterDeleted", isClusterDeleted)
	return isClusterDeleted, nil
}

func (r *RayJobReconciler) suspendWorkerGroups(ctx context.Context, rayJobInstance *rayv1.RayJob) error {
	logger := ctrl.LoggerFrom(ctx)
	clusterIdentifier := common.RayJobRayClusterNamespacedName(rayJobInstance)

	cluster := rayv1.RayCluster{}
	if err := r.Get(ctx, clusterIdentifier, &cluster); err != nil {
		return err
	}

	for i := range cluster.Spec.WorkerGroupSpecs {
		cluster.Spec.WorkerGroupSpecs[i].Suspend = ptr.To[bool](true)
	}

	if err := r.Update(ctx, &cluster); err != nil {
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeWarning,
			string(utils.FailedToUpdateRayCluster),
			"Failed to suspend worker groups in cluster %s/%s: %v",
			cluster.Namespace, cluster.Name, err)
		return err
	}

	logger.Info("All worker groups for RayCluster have had `suspend` set to true", "RayCluster", clusterIdentifier)
	r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, string(utils.UpdatedRayCluster), "Set the `suspend` field to true for all worker groups in cluster %s/%s", cluster.Namespace, cluster.Name)

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayJobReconciler) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayJob{}).
		Owns(&rayv1.RayCluster{}).
		Owns(&corev1.Service{}).
		Owns(&batchv1.Job{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: reconcileConcurrency,
			LogConstructor: func(request *reconcile.Request) logr.Logger {
				logger := ctrl.Log.WithName("controllers").WithName("RayJob")
				if request != nil {
					logger = logger.WithValues("RayJob", request.NamespacedName)
				}
				return logger
			},
		}).
		Complete(r)
}

// This function is the sole place where `JobDeploymentStatusInitializing` is defined. It initializes `Status.JobId` and `Status.RayClusterName`
// prior to job submissions and RayCluster creations. This is used to avoid duplicate job submissions and cluster creations. In addition, this
// function also sets `Status.StartTime` to support `ActiveDeadlineSeconds`.
// This function will set or generate JobId if SubmissionMode is not InteractiveMode.
func initRayJobStatusIfNeed(ctx context.Context, rayJob *rayv1.RayJob) error {
	logger := ctrl.LoggerFrom(ctx)
	shouldUpdateStatus := rayJob.Status.JobId == "" || rayJob.Status.RayClusterName == "" || rayJob.Status.JobStatus == ""
	// Please don't update `shouldUpdateStatus` below.
	logger.Info("initRayJobStatusIfNeed", "shouldUpdateStatus", shouldUpdateStatus, "jobId", rayJob.Status.JobId, "rayClusterName", rayJob.Status.RayClusterName, "jobStatus", rayJob.Status.JobStatus)
	if !shouldUpdateStatus {
		return nil
	}

	if rayJob.Spec.SubmissionMode != rayv1.InteractiveMode && rayJob.Status.JobId == "" {
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
	rayJob.Status.StartTime = &metav1.Time{Time: time.Now()}
	return nil
}

func (r *RayJobReconciler) updateRayJobStatus(ctx context.Context, oldRayJob *rayv1.RayJob, newRayJob *rayv1.RayJob) error {
	logger := ctrl.LoggerFrom(ctx)
	oldRayJobStatus := oldRayJob.Status
	newRayJobStatus := newRayJob.Status
	logger.Info("updateRayJobStatus", "oldRayJobStatus", oldRayJobStatus, "newRayJobStatus", newRayJobStatus)
	// If a status field is crucial for the RayJob state machine, it MUST be
	// updated with a distinct JobStatus or JobDeploymentStatus value.
	if oldRayJobStatus.JobStatus != newRayJobStatus.JobStatus ||
		oldRayJobStatus.JobDeploymentStatus != newRayJobStatus.JobDeploymentStatus {

		if newRayJobStatus.JobDeploymentStatus == rayv1.JobDeploymentStatusComplete || newRayJobStatus.JobDeploymentStatus == rayv1.JobDeploymentStatusFailed {
			newRayJob.Status.EndTime = &metav1.Time{Time: time.Now()}
		}

		logger.Info("updateRayJobStatus", "old JobStatus", oldRayJobStatus.JobStatus, "new JobStatus", newRayJobStatus.JobStatus,
			"old JobDeploymentStatus", oldRayJobStatus.JobDeploymentStatus, "new JobDeploymentStatus", newRayJobStatus.JobDeploymentStatus)
		if err := r.Status().Update(ctx, newRayJob); err != nil {
			return err
		}
	}
	return nil
}

func (r *RayJobReconciler) getOrCreateRayClusterInstance(ctx context.Context, rayJobInstance *rayv1.RayJob) (*rayv1.RayCluster, error) {
	logger := ctrl.LoggerFrom(ctx)
	rayClusterNamespacedName := common.RayJobRayClusterNamespacedName(rayJobInstance)
	logger.Info("try to find existing RayCluster instance", "name", rayClusterNamespacedName.Name)

	rayClusterInstance := &rayv1.RayCluster{}
	if err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("RayCluster not found", "RayCluster", rayClusterNamespacedName)
			if len(rayJobInstance.Spec.ClusterSelector) != 0 {
				err := fmt.Errorf("we have choosed the cluster selector mode, failed to find the cluster named %v, err: %w", rayClusterNamespacedName.Name, err)
				return nil, err
			}

			logger.Info("RayCluster not found, creating RayCluster!", "RayCluster", rayClusterNamespacedName)
			rayClusterInstance, err = r.constructRayClusterForRayJob(rayJobInstance, rayClusterNamespacedName.Name)
			if err != nil {
				return nil, err
			}
			if err := r.Create(ctx, rayClusterInstance); err != nil {
				r.Recorder.Eventf(rayJobInstance, corev1.EventTypeWarning, string(utils.FailedToCreateRayCluster), "Failed to create RayCluster %s/%s: %v", rayClusterInstance.Namespace, rayClusterInstance.Name, err)
				return nil, err
			}
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, string(utils.CreatedRayCluster), "Created RayCluster %s/%s", rayClusterInstance.Namespace, rayClusterInstance.Name)
		} else {
			return nil, err
		}
	}
	logger.Info("Found the associated RayCluster for RayJob", "RayCluster", rayClusterNamespacedName)

	// Verify that RayJob is not in cluster selector mode first to avoid nil pointer dereference error during spec comparison.
	// This is checked by ensuring len(rayJobInstance.Spec.ClusterSelector) equals 0.
	if len(rayJobInstance.Spec.ClusterSelector) == 0 && !utils.CompareJsonStruct(rayClusterInstance.Spec, *rayJobInstance.Spec.RayClusterSpec) {
		logger.Info("Disregard changes in RayClusterSpec of RayJob")
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

func updateStatusToSuspendingIfNeeded(ctx context.Context, rayJob *rayv1.RayJob) bool {
	logger := ctrl.LoggerFrom(ctx)
	if !rayJob.Spec.Suspend {
		return false
	}
	// In KubeRay, only `Running` and `Initializing` are allowed to transition to `Suspending`.
	validTransitions := map[rayv1.JobDeploymentStatus]struct{}{
		rayv1.JobDeploymentStatusRunning:      {},
		rayv1.JobDeploymentStatusInitializing: {},
	}
	if _, ok := validTransitions[rayJob.Status.JobDeploymentStatus]; !ok {
		logger.Info("The current status is not allowed to transition to `Suspending`", "JobDeploymentStatus", rayJob.Status.JobDeploymentStatus)
		return false
	}
	logger.Info("Try to transition the status to `Suspending`", "oldStatus", rayJob.Status.JobDeploymentStatus)
	rayJob.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusSuspending
	return true
}

func checkK8sJobAndUpdateStatusIfNeeded(ctx context.Context, rayJob *rayv1.RayJob, job *batchv1.Job) bool {
	logger := ctrl.LoggerFrom(ctx)
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			logger.Info("The submitter Kubernetes Job has failed. Attempting to transition the status to `Failed`.", "Submitter K8s Job", job.Name, "Reason", cond.Reason, "Message", cond.Message)
			rayJob.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusFailed
			// The submitter Job needs to wait for the user code to finish and retrieve its logs.
			// Therefore, a failed Submitter Job indicates that the submission itself has failed or the user code has thrown an error.
			// If the failure is due to user code, the JobStatus and Job message will be updated accordingly from the previous reconciliation.
			if rayJob.Status.JobStatus == rayv1.JobStatusFailed {
				rayJob.Status.Reason = rayv1.AppFailed
			} else {
				rayJob.Status.Reason = rayv1.SubmissionFailed
				rayJob.Status.Message = fmt.Sprintf("Job submission has failed. Reason: %s. Message: %s", cond.Reason, cond.Message)
			}
			return true
		}
	}
	return false
}

func checkActiveDeadlineAndUpdateStatusIfNeeded(ctx context.Context, rayJob *rayv1.RayJob) bool {
	logger := ctrl.LoggerFrom(ctx)
	if rayJob.Spec.ActiveDeadlineSeconds == nil || time.Now().Before(rayJob.Status.StartTime.Add(time.Duration(*rayJob.Spec.ActiveDeadlineSeconds)*time.Second)) {
		return false
	}

	logger.Info("The RayJob has passed the activeDeadlineSeconds. Transition the status to `Failed`.", "StartTime", rayJob.Status.StartTime, "ActiveDeadlineSeconds", *rayJob.Spec.ActiveDeadlineSeconds)
	rayJob.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusFailed
	rayJob.Status.Reason = rayv1.DeadlineExceeded
	rayJob.Status.Message = fmt.Sprintf("The RayJob has passed the activeDeadlineSeconds. StartTime: %v. ActiveDeadlineSeconds: %d", rayJob.Status.StartTime, *rayJob.Spec.ActiveDeadlineSeconds)
	return true
}
