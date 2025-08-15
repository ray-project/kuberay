package ray

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

type MissedSchedulesType int

const (
	noneMissed MissedSchedulesType = iota
	fewMissed
	manyMissed
)

type RayCronJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	dashboardClientFunc func() utils.RayDashboardClientInterface

	now func() time.Time
}

func NewRayCronJobReconciler(_ context.Context, mgr manager.Manager, provider utils.ClientProvider) *RayCronJobReconciler {
	dashboardClientFunc := provider.GetDashboardClient(mgr)
	return &RayCronJobReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		Recorder:            mgr.GetEventRecorderFor("raycronjob-controller"),
		dashboardClientFunc: dashboardClientFunc,
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

	sched, err := cron.ParseStandard(formatSchedule(rayCronJob, r.Recorder))
	if err != nil {
		// this is likely a user error in defining the spec value
		// we should log the error and not reconcile this cronjob until an update to spec
		logger.V(2).Info("Unparseable schedule", "cronjob", "schedule", rayCronJob.Spec.Schedule, "err", err)
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeWarning, "UnparseableSchedule", "unparseable schedule: %q : %s", rayCronJob.Spec.Schedule, err)
		return ctrl.Result{}, nil
	}

	scheduledTime, err := nextScheduleTime(logger, rayCronJob, r.now(), sched, r.Recorder)
	if err != nil {
		// this is likely a user error in defining the spec value
		// we should log the error and not reconcile this cronjob until an update to spec
		logger.Info("Invalid schedule", "cronjob", "schedule", rayCronJob.Spec.Schedule, "err", err)
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeWarning, "InvalidSchedule", "invalid schedule: %s : %s", rayCronJob.Spec.Schedule, err)
		return ctrl.Result{}, nil
	}

	t1 := lastScheduleTimeDuration(logger, rayCronJob, r.now(), sched)
	if scheduledTime != nil && (*scheduledTime).Before(r.now()) {
		logger.Info("The current time is within the buffer window of a cron tick", "NextScheduleTimeDuration", t1, "Previous LastScheduleTime", rayCronJob.Status.LastScheduleTime)
	} else {
		logger.Info("Waiting until the next reconcile to determine schedule", "nextScheduleDuration", t1, "currentTime", time.Now())
		return ctrl.Result{RequeueAfter: nextScheduleTimeDuration(logger, rayCronJob, r.now(), sched)}, nil
	}

	var childRayJobs rayv1.RayJobList
	if err := r.List(ctx, &childRayJobs, client.InNamespace(rayCronJob.Namespace)); err != nil {
		logger.Error(err, "Unable to list child RayJobs")
		return ctrl.Result{}, err
	}

	rayJobIsActive := false
	for i := range childRayJobs.Items {
		job := &childRayJobs.Items[i]
		// Check if the job is controlled by the current RayCronJob by comparing UIDs.
		if controllerRef := metav1.GetControllerOf(job); controllerRef != nil && controllerRef.UID == rayCronJob.UID {
			if !rayv1.IsJobTerminal(job.Status.JobStatus) {
				rayJobIsActive = true
				break
			}
		}
	}

	if rayJobIsActive {
		logger.Info("Not starting job because a prior execution is still running", "cronjob", rayCronJob.Name)
		r.Recorder.Eventf(rayCronJob, corev1.EventTypeNormal, "JobAlreadyActive", "Not starting job because a prior execution is still running")
		// Requeue for the next scheduled time.
		t := nextScheduleTimeDuration(logger, rayCronJob, r.now(), sched)
		return ctrl.Result{RequeueAfter: t}, nil
	}

	jobReq := getRayJobFromTemplate(rayCronJob, *scheduledTime)

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

	rayCronJob.Status.LastScheduleTime = &metav1.Time{Time: *scheduledTime}

	if err := r.Status().Update(ctx, rayCronJob); err != nil {
		logger.Error(err, "Unable to update rayCronJob status", "jobName", jobReq.Name)
		return ctrl.Result{}, err
	}

	t := nextScheduleTimeDuration(logger, rayCronJob, r.now(), sched)
	return ctrl.Result{RequeueAfter: t}, nil
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

func mostRecentScheduleTime(rj *rayv1.RayCronJob, now time.Time, schedule cron.Schedule) (time.Time, *time.Time, MissedSchedulesType, error) {
	earliestTime := rj.ObjectMeta.CreationTimestamp.Time
	missedSchedules := noneMissed
	if rj.Status.LastScheduleTime != nil {
		earliestTime = rj.Status.LastScheduleTime.Time
	}

	t1 := schedule.Next(earliestTime)
	t2 := schedule.Next(t1)

	if now.Before(t1) {
		return earliestTime, nil, missedSchedules, nil
	}
	if now.Before(t2) {
		return earliestTime, &t1, missedSchedules, nil
	}

	// It is possible for cron.ParseStandard("59 23 31 2 *") to return an invalid schedule
	// minute - 59, hour - 23, dom - 31, month - 2, and dow is optional, clearly 31 is invalid
	// In this case the timeBetweenTwoSchedules will be 0, and we error out the invalid schedule
	timeBetweenTwoSchedules := int64(t2.Sub(t1).Round(time.Second).Seconds())
	if timeBetweenTwoSchedules < 1 {
		return earliestTime, nil, missedSchedules, fmt.Errorf("time difference between two schedules is less than 1 second")
	}
	// this logic used for calculating number of missed schedules does a rough
	// approximation, by calculating a diff between two schedules (t1 and t2),
	// and counting how many of these will fit in between last schedule and now
	timeElapsed := int64(now.Sub(t1).Seconds())
	numberOfMissedSchedules := (timeElapsed / timeBetweenTwoSchedules) + 1

	var mostRecentTime time.Time
	// to get the most recent time accurate for regular schedules and the ones
	// specified with @every form, we first need to calculate the potential earliest
	// time by multiplying the initial number of missed schedules by its interval,
	// this is critical to ensure @every starts at the correct time, this explains
	// the numberOfMissedSchedules-1, the additional -1 serves there to go back
	// in time one more time unit, and let the cron library calculate a proper
	// schedule, for case where the schedule is not consistent, for example
	// something like  30 6-16/4 * * 1-5
	potentialEarliest := t1.Add(time.Duration((numberOfMissedSchedules-1-1)*timeBetweenTwoSchedules) * time.Second)
	for t := schedule.Next(potentialEarliest); !t.After(now); t = schedule.Next(t) {
		mostRecentTime = t
	}

	switch {
	case numberOfMissedSchedules > 100:
		missedSchedules = manyMissed
	// inform about few missed, still
	case numberOfMissedSchedules > 0:
		missedSchedules = fewMissed
	}

	if mostRecentTime.IsZero() {
		return earliestTime, nil, missedSchedules, nil
	}
	return earliestTime, &mostRecentTime, missedSchedules, nil
}

func formatSchedule(rj *rayv1.RayCronJob, recorder record.EventRecorder) string {
	if strings.Contains(rj.Spec.Schedule, "TZ") {
		if recorder != nil {
			recorder.Eventf(rj, corev1.EventTypeWarning, "UnsupportedSchedule", "CRON_TZ or TZ used in schedule %q is not officially supported, see https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/ for more details", rj.Spec.Schedule)
		}

		return rj.Spec.Schedule
	}

	return rj.Spec.Schedule
}

// nextScheduleTimeDuration returns the time duration to requeue a cron schedule based on
// the schedule and last schedule time.
func nextScheduleTimeDuration(logger logr.Logger, rj *rayv1.RayCronJob, now time.Time, schedule cron.Schedule) time.Duration {
	earliestTime, mostRecentTime, missedSchedules, err := mostRecentScheduleTime(rj, now, schedule)
	if err != nil {
		// we still have to requeue at some point, so aim for the next scheduling slot from now
		logger.Info("Error in mostRecentScheduleTime, we still have to requeue at some point, so aim for the next scheduling slot from now", "Error", err)
		mostRecentTime = &now
	} else if mostRecentTime == nil {
		logger.Info("mostRecentTime doesnt exist")
		if missedSchedules == noneMissed {
			// no missed schedules since earliestTime
			mostRecentTime = &earliestTime
		} else {
			// if there are missed schedules since earliestTime, always use now
			mostRecentTime = &now
		}
	}
	logger.Info("Successfully calculated earliestTime and mostRecentTime", "mostRecentTime", mostRecentTime, "Current Time", now, "Next time to aim for", schedule.Next(*mostRecentTime))
	t := schedule.Next(*mostRecentTime).Sub(now)
	return t
}

// The LastScheduleTimeDuration function returns the last previous cron time.
// It calculates the most recent time a schedule should have executed based
// on the RayJob's creation time (or its last scheduled status) and the current time 'now'.
func lastScheduleTimeDuration(logger logr.Logger, rj *rayv1.RayCronJob, now time.Time, schedule cron.Schedule) time.Duration {
	earliestTime, mostRecentTime, missedSchedules, err := mostRecentScheduleTime(rj, now, schedule)
	if err != nil {
		// We still have to requeue at some point, so aim for the next scheduling slot from now
		logger.Info("Error in mostRecentScheduleTime, we still have to requeue at some point", "Error", err)
		mostRecentTime = &now
	} else if mostRecentTime == nil {
		logger.Info("mostRecentTime doesnt exist")
		if missedSchedules == noneMissed {
			// No missed schedules since earliestTime
			mostRecentTime = &earliestTime
		} else {
			// If there are missed schedules since earliestTime, always use now
			mostRecentTime = &now
		}
	}
	return now.Sub(*mostRecentTime)
}

// nextScheduleTime returns the time.Time of the next schedule after the last scheduled
// and before now, or nil if no unmet schedule times, and an error.
// If there are too many (>100) unstarted times, it will also record a warning.
func nextScheduleTime(logger logr.Logger, cj *rayv1.RayCronJob, now time.Time, schedule cron.Schedule, recorder record.EventRecorder) (*time.Time, error) {
	_, mostRecentTime, missedSchedules, err := mostRecentScheduleTime(cj, now, schedule)

	if mostRecentTime == nil || mostRecentTime.After(now) {
		return nil, err
	}

	if missedSchedules == manyMissed {
		recorder.Eventf(cj, corev1.EventTypeWarning, "TooManyMissedTimes", "too many missed start times. Set or decrease .spec.startingDeadlineSeconds or check clock skew")
		logger.Info("too many missed times")
	}

	return mostRecentTime, err
}

// getJobFromTemplate2 makes a Job from a CronJob. It converts the unix time into minutes from
// epoch time and concatenates that to the job name, because the cronjob_controller v2 has the lowest
// granularity of 1 minute for scheduling job.
func getRayJobFromTemplate(cj *rayv1.RayCronJob, scheduledTime time.Time) *rayv1.RayJob {
	labels := copyLabels(&cj.Spec.RayJobTemplate)
	annotations := copyAnnotations(&cj.Spec.RayJobTemplate)
	// We want job names for a given nominal start time to have a deterministic name to avoid the same job being created twice
	name := getJobName(cj, scheduledTime)

	job := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Labels:            labels,
			Annotations:       annotations,
			Name:              name,
			CreationTimestamp: metav1.Time{Time: scheduledTime},
			OwnerReferences:   []metav1.OwnerReference{*metav1.NewControllerRef(cj, rayv1.SchemeGroupVersion.WithKind("RayCronJob"))},
		},
	}
	cj.Spec.RayJobTemplate.Spec.DeepCopyInto(&job.Spec)
	job.Namespace = cj.Namespace
	return job
}

func getJobName(cj *rayv1.RayCronJob, scheduledTime time.Time) string {
	return fmt.Sprintf("%s-%s-%d", "rayjob", cj.Name, getTimeHashInMinutes(scheduledTime))
}

func copyLabels(template *rayv1.RayJobTemplate) labels.Set {
	l := make(labels.Set)
	for k, v := range template.Labels {
		l[k] = v
	}
	return l
}

func copyAnnotations(template *rayv1.RayJobTemplate) labels.Set {
	a := make(labels.Set)
	for k, v := range template.Annotations {
		a[k] = v
	}
	return a
}

// getTimeHashInMinutes returns Unix Epoch Time in minutes
func getTimeHashInMinutes(scheduledTime time.Time) int64 {
	return scheduledTime.Unix() / 60
}
