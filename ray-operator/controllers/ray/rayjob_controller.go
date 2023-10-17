package ray

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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
		if !controllerutil.ContainsFinalizer(rayJobInstance, common.RayJobStopJobFinalizer) {
			r.Log.Info("Add a finalizer", "finalizer", common.RayJobStopJobFinalizer)
			controllerutil.AddFinalizer(rayJobInstance, common.RayJobStopJobFinalizer)
			if err := r.Update(ctx, rayJobInstance); err != nil {
				r.Log.Error(err, "Failed to update RayJob with finalizer")
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
		}
	} else {
		r.Log.Info("RayJob is being deleted", "DeletionTimestamp", rayJobInstance.ObjectMeta.DeletionTimestamp)
		if isJobPendingOrRunning(rayJobInstance.Status.JobStatus) {
			rayDashboardClient := utils.GetRayDashboardClientFunc()
			rayDashboardClient.InitClient(rayJobInstance.Status.DashboardURL)
			err := rayDashboardClient.StopJob(ctx, rayJobInstance.Status.JobId, &r.Log)
			if err != nil {
				r.Log.Info("Failed to stop job for RayJob", "error", err)
			}
		}

		r.Log.Info("Remove the finalizer no matter StopJob() succeeds or not.", "finalizer", common.RayJobStopJobFinalizer)
		controllerutil.RemoveFinalizer(rayJobInstance, common.RayJobStopJobFinalizer)
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

	// Mark the deployment status as Complete if RayJob is succeed or failed
	// TODO: (jiaxin.shan) Double check raycluster status to make sure we don't have create duplicate clusters..
	// But the code here is not elegant. We should spend some time to refactor the flow.
	if isJobSucceedOrFailed(rayJobInstance.Status.JobStatus) && rayJobInstance.Status.JobDeploymentStatus != rayv1.JobDeploymentStatusComplete {
		// We need to make sure the cluster is deleted or in deletion, then update the status.
		rayClusterInstance := &rayv1.RayCluster{}
		rayClusterNamespacedName := types.NamespacedName{
			Namespace: rayJobInstance.Namespace,
			Name:      rayJobInstance.Status.RayClusterName,
		}
		if err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance); err != nil {
			if !errors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			if err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusComplete, nil); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		if rayClusterInstance.DeletionTimestamp != nil {
			if err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusComplete, nil); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Set rayClusterName and rayJobId first, to avoid duplicate submission
	err = r.setRayJobIdAndRayClusterNameIfNeed(ctx, rayJobInstance)
	if err != nil {
		r.Log.Error(err, "failed to set jobId or rayCluster name", "RayJob", request.NamespacedName)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	var rayClusterInstance *rayv1.RayCluster
	if rayClusterInstance, err = r.getOrCreateRayClusterInstance(ctx, rayJobInstance); err != nil {
		err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusFailedToGetOrCreateRayCluster, err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}
	// If there is no cluster instance and no error suspend the job deployment
	if rayClusterInstance == nil {
		// Already suspended?
		if rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusSuspended {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusSuspended, err)
		if err != nil {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		r.Log.Info("rayJob suspended", "RayJob", rayJobInstance.Name)
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Suspended", "Suspended RayJob %s", rayJobInstance.Name)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Always update RayClusterStatus along with jobStatus and jobDeploymentStatus updates.
	rayJobInstance.Status.RayClusterStatus = rayClusterInstance.Status

	rayDashboardClient := utils.GetRayDashboardClientFunc()
	if clientURL := rayJobInstance.Status.DashboardURL; clientURL == "" {
		// TODO: dashboard service may be changed. Check it instead of using the same URL always
		if clientURL, err = utils.FetchHeadServiceURL(ctx, &r.Log, r.Client, rayClusterInstance, common.DashboardPortName); err != nil || clientURL == "" {
			if clientURL == "" {
				err = fmt.Errorf("empty dashboardURL")
			}
			err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusWaitForDashboard, err)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		// Check the dashboard readiness by checking the err return from rayDashboardClient.GetJobInfo.
		// Note that rayDashboardClient.GetJobInfo returns no error in the case of http.StatusNotFound.
		// This check is a workaround for https://github.com/ray-project/kuberay/issues/1381.
		rayDashboardClient.InitClient(clientURL)
		if _, err = rayDashboardClient.GetJobInfo(ctx, rayJobInstance.Status.JobId); err != nil {
			err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusWaitForDashboardReady, err)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		rayJobInstance.Status.DashboardURL = clientURL
	} else {
		rayDashboardClient.InitClient(clientURL)
	}

	// Check the current status of ray cluster before submitting.
	if rayClusterInstance.Status.State != rayv1.Ready {
		r.Log.Info("waiting for the cluster to be ready", "rayCluster", rayClusterInstance.Name)
		err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusInitializing, nil)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Ensure k8s job has been created
	jobName, wasJobCreated, err := r.getOrCreateK8sJob(ctx, rayJobInstance, rayClusterInstance)
	if err != nil {
		err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusFailedJobDeploy, err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	if wasJobCreated {
		r.Log.Info("K8s job successfully created", "RayJob", rayJobInstance.Name, "jobId", jobName)
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Created", "Created k8s job %s", jobName)
	} else {
		r.Log.Info("K8s job successfully retrieved", "RayJob", rayJobInstance.Name, "jobId", jobName)
	}

	// Check the status of the k8s job and update the RayJobInstance status accordingly.
	// Get the k8s job
	k8sJob := &batchv1.Job{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: jobName, Namespace: rayJobInstance.Namespace}, k8sJob)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Job not found", "RayJob", rayJobInstance.Name, "jobId", jobName)
			err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusWaitForK8sJob, err)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		r.Log.Error(err, "failed to get k8s job")
		err = r.updateState(ctx, rayJobInstance, nil, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusFailedToGetJobStatus, err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Check the current status of ray jobs
	jobInfo, err := rayDashboardClient.GetJobInfo(ctx, rayJobInstance.Status.JobId)
	if err != nil {
		err = r.updateState(ctx, rayJobInstance, jobInfo, rayJobInstance.Status.JobStatus, rayv1.JobDeploymentStatusFailedToGetJobStatus, err)
		// Dashboard service in head pod takes time to start, it's possible we get connection refused error.
		// Requeue after few seconds to avoid continuous connection errors.
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	// Update RayJob.Status (Kubernetes CR) from Ray Job Status from Dashboard service
	if jobInfo != nil && jobInfo.JobStatus != rayJobInstance.Status.JobStatus {
		r.Log.Info(fmt.Sprintf("Update status from %s to %s", rayJobInstance.Status.JobStatus, jobInfo.JobStatus), "rayjob", rayJobInstance.Status.JobId)
		err = r.updateState(ctx, rayJobInstance, jobInfo, jobInfo.JobStatus, rayv1.JobDeploymentStatusRunning, nil)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	if rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusRunning {
		// If suspend flag is set AND
		// the RayJob is submitted against the RayCluster created by THIS job, then
		// try to gracefully stop the Ray job and delete (suspend) the cluster
		if rayJobInstance.Spec.Suspend && len(rayJobInstance.Spec.ClusterSelector) == 0 {
			info, err := rayDashboardClient.GetJobInfo(ctx, rayJobInstance.Status.JobId)
			if err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			if !rayv1.IsJobTerminal(info.JobStatus) {
				err := rayDashboardClient.StopJob(ctx, rayJobInstance.Status.JobId, &r.Log)
				if err != nil {
					return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
				}
			}
			if info.JobStatus != rayv1.JobStatusStopped {
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
			err = r.updateState(ctx, rayJobInstance, jobInfo, rayv1.JobStatusStopped, rayv1.JobDeploymentStatusSuspended, nil)
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
	if isJobSucceedOrFailed(rayJobInstance.Status.JobStatus) && rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusRunning {
		if rayJobInstance.Spec.ShutdownAfterJobFinishes {
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
	return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
}

// getOrCreateK8sJob creates a Kubernetes Job for the Ray Job if it doesn't exist, otherwise returns the existing one. It returns the Job name and a boolean indicating whether the Job was created.
func (r *RayJobReconciler) getOrCreateK8sJob(ctx context.Context, rayJobInstance *rayv1.RayJob, rayClusterInstance *rayv1.RayCluster) (string, bool, error) {
	jobName := rayJobInstance.Name
	jobNamespace := rayJobInstance.Namespace

	// Create a Job object with the specified name and namespace
	job := &batchv1.Job{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: jobNamespace, Name: jobName}, job); err != nil {
		if errors.IsNotFound(err) {
			submitterTemplate, err := r.getSubmitterTemplate(rayJobInstance, rayClusterInstance)
			if err != nil {
				r.Log.Error(err, "failed to get submitter template")
				return "", false, err
			}
			return r.createNewK8sJob(ctx, rayJobInstance, submitterTemplate, rayClusterInstance)
		}

		// Some other error occurred while trying to get the Job
		r.Log.Error(err, "failed to get k8s Job")
		return "", false, err
	}

	// Job already exists, instead of returning an error we return a "success"
	return jobName, false, nil
}

// getSubmitterTemplate builds the submitter pod template for the Ray job.
func (r *RayJobReconciler) getSubmitterTemplate(rayJobInstance *rayv1.RayJob, rayClusterInstance *rayv1.RayCluster) (v1.PodTemplateSpec, error) {
	var submitterTemplate v1.PodTemplateSpec

	// Set the default value for the optional field SubmitterPodTemplate if not provided.
	if rayJobInstance.Spec.SubmitterPodTemplate == nil {
		submitterTemplate = common.GetDefaultSubmitterTemplate(rayClusterInstance)
		r.Log.Info("default submitter template is used")
	} else {
		submitterTemplate = *rayJobInstance.Spec.SubmitterPodTemplate.DeepCopy()
		r.Log.Info("user-provided submitter template is used; the first container is assumed to be the submitter")
	}

	// If the command in the submitter pod template isn't set, use the default command.
	if len(submitterTemplate.Spec.Containers[common.RayContainerIndex].Command) == 0 {
		// Check for deprecated 'runtimeEnv' field usage and log a warning.
		if len(rayJobInstance.Spec.RuntimeEnv) > 0 {
			r.Log.Info("Warning: The 'runtimeEnv' field is deprecated. Please use 'runtimeEnvYAML' instead.")
		}

		k8sJobCommand, err := common.GetK8sJobCommand(rayJobInstance)
		if err != nil {
			return v1.PodTemplateSpec{}, err
		}
		submitterTemplate.Spec.Containers[common.RayContainerIndex].Command = k8sJobCommand
		r.Log.Info("No command is specified in the user-provided template. Default command is used", "command", k8sJobCommand)
	} else {
		r.Log.Info("User-provided command is used", "command", submitterTemplate.Spec.Containers[common.RayContainerIndex].Command)
	}

	// Set PYTHONUNBUFFERED=1 for real-time logging
	submitterTemplate.Spec.Containers[common.RayContainerIndex].Env = append(submitterTemplate.Spec.Containers[common.RayContainerIndex].Env, v1.EnvVar{
		Name:  PythonUnbufferedEnvVarName,
		Value: "1",
	})

	return submitterTemplate, nil
}

// createNewK8sJob creates a new Kubernetes Job. It returns the Job's name and a boolean indicating whether a new Job was created.
func (r *RayJobReconciler) createNewK8sJob(ctx context.Context, rayJobInstance *rayv1.RayJob, submitterTemplate v1.PodTemplateSpec, rayClusterInstance *rayv1.RayCluster) (string, bool, error) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rayJobInstance.Name,
			Namespace: rayJobInstance.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: submitterTemplate,
		},
	}

	// Set the ownership in order to do the garbage collection by k8s.
	if err := ctrl.SetControllerReference(rayClusterInstance, job, r.Scheme); err != nil {
		r.Log.Error(err, "failed to set controller reference")
		return "", false, err
	}

	// Create the Kubernetes Job
	if err := r.Client.Create(ctx, job); err != nil {
		r.Log.Error(err, "failed to create k8s Job")
		return "", false, err
	}

	// Return the Job's name and true indicating a new job was created
	return job.Name, true, nil
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

// isJobSucceedOrFailed indicates whether the job comes into end status.
func isJobSucceedOrFailed(status rayv1.JobStatus) bool {
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
		Complete(r)
}

func (r *RayJobReconciler) setRayJobIdAndRayClusterNameIfNeed(ctx context.Context, rayJob *rayv1.RayJob) error {
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
		// if the clusterSelector is not empty, default use this cluster name
		// we assume the length of clusterSelector is one
		if len(rayJob.Spec.ClusterSelector) != 0 {
			var useValue string
			var ok bool
			if useValue, ok = rayJob.Spec.ClusterSelector[RayJobDefaultClusterSelectorKey]; !ok {
				return fmt.Errorf("failed to get cluster name in ClusterSelector map, the default key is %v", RayJobDefaultClusterSelectorKey)
			}
			rayJob.Status.RayClusterName = useValue
			rayJob.Spec.ShutdownAfterJobFinishes = false
		} else {
			rayJob.Status.RayClusterName = utils.GenerateRayClusterName(rayJob.Name)
		}
	}

	if shouldUpdateStatus {
		return r.updateState(ctx, rayJob, nil, rayJob.Status.JobStatus, rayv1.JobDeploymentStatusInitializing, nil)
	}
	return nil
}

// make sure the priority is correct
func (r *RayJobReconciler) updateState(ctx context.Context, rayJob *rayv1.RayJob, jobInfo *utils.RayJobInfo, jobStatus rayv1.JobStatus, jobDeploymentStatus rayv1.JobDeploymentStatus, err error) error {
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
		if jobInfo.StartTime >= jobInfo.EndTime {
			rayJob.Status.EndTime = nil
		} else {
			rayJob.Status.EndTime = utils.ConvertUnixTimeToMetav1Time(jobInfo.EndTime)
		}
	}

	// TODO (kevin85421): ObservedGeneration should be used to determine whether update this CR or not.
	rayJob.Status.ObservedGeneration = rayJob.ObjectMeta.Generation

	if errStatus := r.Status().Update(ctx, rayJob); errStatus != nil {
		return fmtErrors.Errorf("combined error: %v %v", err, errStatus)
	}
	return err
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

		r.Log.Info("Update ray cluster replica", "raycluster", rayClusterNamespacedName)
		if err := r.Update(ctx, rayClusterInstance); err != nil {
			r.Log.Error(err, "Fail to update ray cluster replica!", "rayCluster", rayClusterNamespacedName)
			// Error updating the RayCluster object.
			return nil, client.IgnoreNotFound(err)
		}

	} else if errors.IsNotFound(err) {
		// TODO: If both ClusterSelector and RayClusterSpec are not set, we avoid should attempting to retrieve a RayCluster instance.
		// Consider moving this logic to a more appropriate location.
		if len(rayJobInstance.Spec.ClusterSelector) == 0 && rayJobInstance.Spec.RayClusterSpec == nil {
			err := fmt.Errorf("Both ClusterSelector and RayClusterSpec are undefined")
			r.Log.Error(err, "Failed to configure RayCluster instance due to missing configuration")
			return nil, err
		}

		if len(rayJobInstance.Spec.ClusterSelector) != 0 {
			err := fmt.Errorf("we have choosed the cluster selector mode, failed to find the cluster named %v, err: %v", rayClusterInstanceName, err)
			r.Log.Error(err, "Get rayCluster instance error!")
			return nil, err
		}

		// special case: is the job is complete status and cluster has been recycled.
		if isJobSucceedOrFailed(rayJobInstance.Status.JobStatus) && rayJobInstance.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusComplete {
			r.Log.Info("The cluster has been recycled for the job, skip duplicate creation", "rayjob", rayJobInstance.Name)
			return nil, err
		}
		// special case: don't create a cluster instance and don't return an error if the suspend flag of the job is true
		if rayJobInstance.Spec.Suspend {
			return nil, nil
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
