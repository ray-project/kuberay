package ray

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ref "k8s.io/client-go/tools/reference"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	ScheduleBuffer = 100 * time.Millisecond
)

type RayCronJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	dashboardClientFunc func() utils.RayDashboardClientInterface
	options             RayCronJobReconcilerOptions

	now func() time.Time
}

type RayCronJobReconcilerOptions struct {
	RayCronJobMetricsManager *metrics.RayCronJobMetricsManager
}

func NewRayCronJobReconciler(_ context.Context, mgr manager.Manager, options RayCronJobReconcilerOptions, provider utils.ClientProvider) *RayCronJobReconciler {
	dashboardClientFunc := provider.GetDashboardClient(mgr)
	return &RayCronJobReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		Recorder:            mgr.GetEventRecorderFor("raycronjob-controller"),
		dashboardClientFunc: dashboardClientFunc,
		options:             options,
		now:                 time.Now,
	}
}

// +kubebuilder:rbac:groups=ray.io,resources=raycronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=raycronjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=raycronjobs/finalizers,verbs=update
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
func (r *RayCronJobReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	var err error
	rayCronJob := &rayv1.RayCronJob{}
	if err := r.Get(ctx, request.NamespacedName, rayCronJob); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request. Stop reconciliation.
			logger.Info("RayJob resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get RayJob")
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}
	if err := utils.ValidateRayCronJobMetadata(rayCronJob.ObjectMeta); err != nil {
		logger.Error(err, "The RayJob metadata is invalid")
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeWarning, string(utils.InvalidRayJobMetadata),
			"The RayJob metadata is invalid %s/%s: %v", rayCronJob.Namespace, rayCronJob.Name, err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	if err := utils.ValidateRayCronJobSpec(rayCronJob); err != nil {
		logger.Error(err, "The RayJob spec is invalid")
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeWarning, string(utils.InvalidRayJobSpec),
			"The RayJob spec is invalid %s/%s: %v", rayCronJob.Namespace, rayCronJob.Name, err)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	}

	newActiveJobs := []corev1.ObjectReference{}
	for _, activeJobRef := range rayCronJob.Status.Active {
		activeJob := &rayv1.RayJob{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: activeJobRef.Name, Namespace: activeJobRef.Namespace}, activeJob)
		if err != nil {
			if errors.IsNotFound(err) {
				// Job was deleted externally. Remove it from the active list.
				logger.Info("Active RayJob not found, assuming it was deleted", "jobName", activeJobRef.Name)
				continue
			}
			logger.Error(err, "Error getting active job", "jobName", activeJobRef.Name)
			return ctrl.Result{}, err
		}

		// Check if the job is finished (succeeded or failed)
		if activeJob.Status.JobStatus == rayv1.JobStatusSucceeded || activeJob.Status.JobStatus == rayv1.JobStatusFailed {
			logger.Info("Active RayJob has finished", "jobName", activeJobRef.Name, "status", activeJob.Status.JobStatus)
		} else {
			// If the job is still running, keep it in the new active list.
			newActiveJobs = append(newActiveJobs, activeJobRef)
		}

	}
	rayCronJob.Status.Active = newActiveJobs

	// oldRayCronJob := rayCronJob.DeepCopy()
	// If suspend is true then we dont do any logic and wait for next request
	if rayCronJob.Spec.Suspend != nil && *rayCronJob.Spec.Suspend {
		logger.Info("Not starting job because the cron is suspended")
		return ctrl.Result{}, err
	}

	sched, err := cron.ParseStandard(utils.FormatSchedule(rayCronJob, r.Recorder))
	if err != nil {
		// this is likely a user error in defining the spec value
		// we should log the error and not reconcile this cronjob until an update to spec
		logger.V(2).Info("Unparseable schedule", "cronjob", "schedule", rayCronJob.Spec.Schedule, "err", err)
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeWarning, "UnparseableSchedule", "unparseable schedule: %q : %s", rayCronJob.Spec.Schedule, err)
		return ctrl.Result{}, nil
	}

	scheduledTime, err := utils.NextScheduleTime(logger, rayCronJob, r.now(), sched, r.Recorder)
	if err != nil {
		// this is likely a user error in defining the spec value
		// we should log the error and not reconcile this cronjob until an update to spec
		logger.Info("Invalid schedule", "cronjob", "schedule", rayCronJob.Spec.Schedule, "err", err)
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeWarning, "InvalidSchedule", "invalid schedule: %s : %s", rayCronJob.Spec.Schedule, err)
		return ctrl.Result{}, nil
	}

	t1 := utils.LastScheduleTimeDuration(logger, rayCronJob, r.now(), sched)
	if t1 <= ScheduleBuffer {
		logger.Info("The current time is within the buffer window of a cron tick", "NextScheduleTimeDuration", t1, "Previous LastScheduleTime", rayCronJob.Status.LastScheduleTime)
	} else {
		logger.Info("Waiting until the next reconcile to determine schedule", "nextScheduleDuration", t1, "currentTime", time.Now())
		return ctrl.Result{RequeueAfter: utils.NextScheduleTimeDuration(logger, rayCronJob, r.now(), sched)}, nil
	}

	if rayCronJob.Spec.ConcurrencyPolicy == rayv1.ForbidConcurrent && len(rayCronJob.Status.Active) > 0 {
		// Regardless which source of information we use for the set of active jobs,
		// there is some risk that we won't see an active job when there is one.
		// (because we haven't seen the status update to the SJ or the created pod).
		// So it is theoretically possible to have concurrency with Forbid.
		// As long the as the invocations are "far enough apart in time", this usually won't happen.
		//
		// TODO: for Forbid, we could use the same name for every execution, as a lock.
		// With replace, we could use a name that is deterministic per execution time.
		// But that would mean that you could not inspect prior successes or failures of Forbid jobs.
		logger.Info("Not starting job because prior execution is still running and concurrency policy is Forbid", "cronjob")
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeNormal, "JobAlreadyActive", "Not starting job because prior execution is running and concurrency policy is Forbid")
		t := utils.NextScheduleTimeDuration(logger, rayCronJob, r.now(), sched)
		return ctrl.Result{RequeueAfter: t}, nil
	}

	if rayCronJob.Spec.ConcurrencyPolicy == rayv1.ReplaceConcurrent {
		for _, j := range rayCronJob.Status.Active {
			logger.Info("Deleting job that was still running at next scheduled start time")
			job := &rayv1.RayJob{}
			err := r.Client.Get(ctx, types.NamespacedName{Name: j.Name, Namespace: j.Namespace}, job)
			if err != nil {
				// If the job is already gone, just log it and continue.
				r.Recorder.Eventf(rayCronJob, corev1.EventTypeWarning, "FailedGet", "Error getting active job %s: %v", j.Name, err)
				return ctrl.Result{}, fmt.Errorf("error getting active job %s: %w", j.Name, err)
			}

			if !r.deleteJob(ctx, logger, rayCronJob, j, r.Recorder) {
				return ctrl.Result{}, fmt.Errorf("could not replace job %s/%s", job.Namespace, job.Name)
			}
		}
	}

	jobReq, err := utils.GetRayJobFromTemplate2(rayCronJob, *scheduledTime)
	if err != nil {
		logger.Error(err, "Unable to make Job from template", "cronjob")
		return ctrl.Result{}, err
	}
	err = r.Client.Create(ctx, jobReq)
	if err != nil {
		// If the job already exists, it's possible this controller created it before crashing.
		// We'll try to fetch it and proceed as if we just created it.
		if errors.IsAlreadyExists(err) {
			logger.Info("RayJob already exists, fetching the existing one.", "jobName", jobReq.Name)
			existingJob := &rayv1.RayJob{}
			if getErr := r.Client.Get(ctx, types.NamespacedName{Name: jobReq.Name, Namespace: jobReq.Namespace}, existingJob); getErr != nil {
				logger.Error(getErr, "Failed to get existing RayJob", "jobName", jobReq.Name)
				return ctrl.Result{}, getErr
			}
		} else {
			// For other creation errors, record an event and requeue.
			logger.Error(err, "Failed to create new RayJob", "jobName", jobReq.Name)
			r.Recorder.Eventf(rayCronJob, corev1.EventTypeWarning, "FailedCreate", "Error creating RayJob: %v", err)
			return ctrl.Result{}, err
		}
	} else {
		// If creation was successful, log it and record an event.
		logger.Info("Successfully created RayJob")
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeNormal, "SuccessfulCreate", "Created RayJob %s")
	}

	jobRef, err := ref.GetReference(r.Scheme, jobReq)
	if err != nil {
		logger.Error(err, "Unable to get object reference for new RayJob", "jobName", jobReq.Name)
		return ctrl.Result{}, err
	}
	rayCronJob.Status.Active = append(rayCronJob.Status.Active, *jobRef)
	rayCronJob.Status.LastScheduleTime = &metav1.Time{Time: *scheduledTime}

	if err := r.Status().Update(ctx, rayCronJob); err != nil {
		logger.Error(err, "Unable to update rayCronJob status", "jobName", jobReq.Name)
		return ctrl.Result{}, err
	}

	t := utils.NextScheduleTimeDuration(logger, rayCronJob, r.now(), sched)
	return ctrl.Result{RequeueAfter: t}, nil
}

// The receiver (r *RayCronJobReconciler) attaches this function to the struct, making it a method.
func (r *RayCronJobReconciler) deleteJob(ctx context.Context, logger klog.Logger, cj *rayv1.RayCronJob, jobref corev1.ObjectReference, recorder record.EventRecorder) bool {
	// Create a job object to delete. We only need the name and namespace.
	jobToDelete := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobref.Name,
			Namespace: jobref.Namespace,
		},
	}

	// Use the reconciler's client to delete the job.
	if err := r.Client.Delete(ctx, jobToDelete); err != nil {
		// If the job is already gone, don't treat it as an error.
		if errors.IsNotFound(err) {
			return true
		}
		recorder.Eventf(cj, corev1.EventTypeWarning, "FailedDelete", "Error deleting job %s: %v", jobref.Name, err)
		logger.Error(err, "Error deleting job", "job", klog.KObj(jobToDelete))
		return false
	}

	// Remove the job's reference from the active list in the CronJob's status.
	utils.DeleteFromActiveList(cj, jobref.UID)
	recorder.Eventf(cj, corev1.EventTypeNormal, "SuccessfulDelete", "Deleted job %s", jobref.Name)

	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayCronJobReconciler) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayCronJob{}).
		Owns(&rayv1.RayJob{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: reconcileConcurrency,
			LogConstructor: func(request *reconcile.Request) logr.Logger {
				logger := ctrl.Log.WithName("controllers").WithName("RayCronJob")
				if request != nil {
					logger = logger.WithValues("RayCronJob", request.NamespacedName)
				}
				return logger
			},
		}).
		Complete(r)
}
