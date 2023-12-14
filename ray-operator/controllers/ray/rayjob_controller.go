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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

	if rayJobInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object.
		if !controllerutil.ContainsFinalizer(rayJobInstance, utils.RayJobStopJobFinalizer) {
			r.Log.Info("Add a finalizer", "finalizer", utils.RayJobStopJobFinalizer)
			controllerutil.AddFinalizer(rayJobInstance, utils.RayJobStopJobFinalizer)
			if err := r.Update(ctx, rayJobInstance); err != nil {
				r.Log.Error(err, "Failed to update RayJob with finalizer")
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
		}
	} else {
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

	// Do not reconcile the RayJob if the deployment status is marked as Complete
	if rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusComplete {
		r.Log.Info("rayjob is complete, skip reconciliation", "rayjob", rayJobInstance.Name)
		return ctrl.Result{}, nil
	}

	// If the JobStatus is in the SUCCEEDED or FAILED, it is impossible for the Ray job to transition to any other status
	// because both of them are terminal status. Additionally, RayJob does not currently support retries. Hence, we can
	// mark the RayJob as "Complete" to avoid unnecessary reconciliation. Note that the definition of "Complete" does not
	// include STOPPED which is also a terminal status because `suspend` requires to stop the Ray job gracefully before
	// delete the RayCluster.
	if isJobSucceedOrFail(rayJobInstance.Status.JobStatus) {
		// If the function `updateState` updates the JobStatus to Complete successfully, we can skip the reconciliation.
		rayClusterInstance := &rayv1.RayCluster{}
		rayClusterNamespacedName := types.NamespacedName{
			Namespace: rayJobInstance.Namespace,
			Name:      rayJobInstance.Status.RayClusterName,
		}
		if err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance); err != nil {
			if !errors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			if err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusComplete); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			return ctrl.Result{}, nil
		}

		if rayClusterInstance.DeletionTimestamp != nil {
			if err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusComplete); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			return ctrl.Result{}, nil
		}
	}

	// Set `Status.JobDeploymentStatus` to `JobDeploymentStatusInitializing`, and initialize `Status.JobId`
	// and `Status.RayClusterName` prior to avoid duplicate job submissions and cluster creations.
	if err = r.initRayJobStatusIfNeed(ctx, rayJobInstance); err != nil {
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	var rayClusterInstance *rayv1.RayCluster
	if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayJobInstance); err != nil {
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}
	// If there is no cluster instance and no error suspend the job deployment
	if rayClusterInstance == nil {
		// Already suspended?
		if rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusSuspended {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
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

	if jobInfo != nil {
		// TODO (kevin85421): `GetJobInfo` should not return both JobInfo and error with nil values,
		// but it does when the job is not found. This check is a workaround to avoid dereferencing
		// a nil pointer.
		err = r.updateState(ctx, rayJobInstance, jobInfo, jobInfo.JobStatus, rayv1.JobDeploymentStatusRunning)
	}

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

			_, err = r.deleteCluster(ctx, rayJobInstance)
			if err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
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
		if isJobPendingOrRunning(jobInfo.JobStatus) {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
		}
	}

	// Let's use rayJobInstance.Status.JobStatus to make sure we only delete cluster after the CR is updated.
	if isJobSucceedOrFail(rayJobInstance.Status.JobStatus) && rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusRunning {
		if rayJobInstance.Spec.ShutdownAfterJobFinishes && len(rayJobInstance.Spec.ClusterSelector) == 0 {
			// the RayJob is submitted against the RayCluster created by THIS job, so we can tear that
			// RayCluster down.
			if rayJobInstance.Spec.TTLSecondsAfterFinished != nil {
				r.Log.V(3).Info("TTLSecondsAfterSetting", "end_time", rayJobInstance.Status.EndTime.Time, "now", time.Now(), "ttl", *rayJobInstance.Spec.TTLSecondsAfterFinished)
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
			_, err = r.deleteCluster(ctx, rayJobInstance)
			if err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
			}
		}
	}

	// TODO (kevin85421): Use the source of truth `jobInfo.JobStatus` instead.
	if isJobPendingOrRunning(rayJobInstance.Status.JobStatus) {
		// Requeue the RayJob to poll its status from the running Ray job
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
	}
	// Otherwise only reconcile the RayJob upon new events for watched resources
	// to avoid infinite reconciliation.
	return ctrl.Result{}, nil
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

// createNewK8sJob creates a new Kubernetes Job. It returns the Job's name and an error.
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

func (r *RayJobReconciler) deleteCluster(ctx context.Context, rayJobInstance *rayv1.RayJob) (reconcile.Result, error) {
	clusterIdentifier := types.NamespacedName{
		Name:      rayJobInstance.Status.RayClusterName,
		Namespace: rayJobInstance.Namespace,
	}
	cluster := rayv1.RayCluster{}
	if err := r.Get(ctx, clusterIdentifier, &cluster); err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		r.Log.Info("The associated cluster has been already deleted and it can not be found", "RayCluster", clusterIdentifier)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	} else {
		if cluster.DeletionTimestamp != nil {
			r.Log.Info("The cluster deletion is ongoing.", "rayjob", rayJobInstance.Name, "raycluster", cluster.Name)
		} else {
			if err := r.Delete(ctx, &cluster); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			r.Log.Info("The associated cluster is deleted", "RayCluster", clusterIdentifier)
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Deleted", "Deleted cluster %s", rayJobInstance.Status.RayClusterName)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
		}
	}
	return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
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
		rayJob.Status.JobStatus = rayv1.JobStatusPending
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

// TODO: select existing rayclusters by ClusterSelector
func (r *RayJobReconciler) getOrCreateRayClusterInstance(ctx context.Context, rayJobInstance *rayv1.RayJob) (*rayv1.RayCluster, error) {
	rayClusterInstanceName := rayJobInstance.Status.RayClusterName
	r.Log.V(3).Info("try to find existing RayCluster instance", "name", rayClusterInstanceName)
	rayClusterNamespacedName := types.NamespacedName{
		Namespace: rayJobInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1.RayCluster{}
	err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance)
	if err == nil {
		r.Log.Info("Found associated RayCluster for RayJob", "rayjob", rayJobInstance.Name, "raycluster", rayClusterNamespacedName)

		// Case1: The job is submitted to an existing ray cluster, simply return the rayClusterInstance.
		// We do not use rayJobInstance.Spec.RayClusterSpec == nil to check if the cluster selector mode is activated.
		// This is because a user might set both RayClusterSpec and ClusterSelector. with rayJobInstance.Spec.RayClusterSpec == nil,
		// though the RayJob controller will still use ClusterSelector, but it's now able to update the replica.
		// this could result in a conflict as both the RayJob controller and the autoscaler in the existing RayCluster might try to update replicas simultaneously.
		if len(rayJobInstance.Spec.ClusterSelector) != 0 {
			r.Log.Info("ClusterSelector is being used to select an existing RayCluster. RayClusterSpec will be disregarded", "raycluster", rayClusterNamespacedName)
			return rayClusterInstance, nil
		}

		// Note, unlike the RayService, which creates new Ray clusters if any spec is changed,
		// RayJob only supports changing the replicas. Changes to other specs may lead to
		// unexpected behavior. Therefore, the following code focuses solely on updating replicas.

		// Case2: In-tree autoscaling is enabled, only the autoscaler should update replicas to prevent race conditions
		// between user updates and autoscaler decisions. RayJob controller should not modify the replica. Consider this scenario:
		// 1. The autoscaler updates replicas to 10 based on the current workload.
		// 2. The user updates replicas to 15 in the RayJob YAML file.
		// 3. Both RayJob controller and the autoscaler attempt to update replicas, causing worker pods to be repeatedly created and terminated.
		if rayJobInstance.Spec.RayClusterSpec.EnableInTreeAutoscaling != nil && *rayJobInstance.Spec.RayClusterSpec.EnableInTreeAutoscaling {
			// Note, currently, there is no method to verify if the user has updated the RayJob since the last reconcile.
			// In future, we could utilize annotation that stores the hash of the RayJob since last reconcile to compare.
			// For now, we just log a warning message to remind the user regadless whether user has updated RayJob.
			r.Log.Info("Since in-tree autoscaling is enabled, any adjustments made to the RayJob will be disregarded and will not be propagated to the RayCluster.")
			return rayClusterInstance, nil
		}

		// Case3: In-tree autoscaling is disabled, respect the user's replicas setting.
		// Loop over all worker groups and update replicas.
		areReplicasIdentical := true
		for i := range rayJobInstance.Spec.RayClusterSpec.WorkerGroupSpecs {
			if *rayClusterInstance.Spec.WorkerGroupSpecs[i].Replicas != *rayJobInstance.Spec.RayClusterSpec.WorkerGroupSpecs[i].Replicas {
				areReplicasIdentical = false
				*rayClusterInstance.Spec.WorkerGroupSpecs[i].Replicas = *rayJobInstance.Spec.RayClusterSpec.WorkerGroupSpecs[i].Replicas
			}
		}

		// Other specs rather than replicas are changed, warn the user that the RayJob supports replica changes only.
		if !utils.CompareJsonStruct(rayClusterInstance.Spec, *rayJobInstance.Spec.RayClusterSpec) {
			r.Log.Info("RayJob supports replica changes only. Adjustments made to other specs will be disregarded as they may cause unexpected behavior")
		}

		// Avoid updating the RayCluster's replicas if it's identical to the RayJob's replicas.
		if areReplicasIdentical {
			return rayClusterInstance, nil
		}

		r.Log.Info("Update RayCluster replica", "RayCluster", rayClusterNamespacedName)
		if err := r.Update(ctx, rayClusterInstance); err != nil {
			r.Log.Error(err, "Fail to update RayCluster replica!", "RayCluster", rayClusterNamespacedName)
			// Error updating the RayCluster object.
			return nil, client.IgnoreNotFound(err)
		}

	} else if errors.IsNotFound(err) {
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

		r.Log.Info("RayCluster not found, creating RayCluster!", "raycluster", rayClusterNamespacedName)
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
