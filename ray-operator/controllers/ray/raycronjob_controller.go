package ray

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	RayCronJobDefaultRequeueDuration = 3 * time.Second
)

// RayCronJobReconciler reconciles a RayCronJob object
type RayCronJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	clock    clock.Clock
}

// NewRayCronJobReconciler returns a new RayCronJobReconciler
func NewRayCronJobReconciler(mgr ctrl.Manager) *RayCronJobReconciler {
	return &RayCronJobReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("raycronjob-controller"),
		clock:    clock.RealClock{},
	}
}

//+kubebuilder:rbac:groups=ray.io,resources=raycronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ray.io,resources=raycronjobs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ray.io,resources=raycronjobs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RayCronJob object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *RayCronJobReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	// Get RayCronJob instance
	rayCronJobInstance := &rayv1.RayCronJob{}
	if err := r.Get(ctx, request.NamespacedName, rayCronJobInstance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request. Stop reconciliation.
			logger.Info("RayCronJob resource not found.")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get RayCronJob")
		return ctrl.Result{RequeueAfter: RayCronJobDefaultRequeueDuration}, err
	}

	// Please do NOT modify `originalRayCronJobInstance` in the following code.
	originalRayCronJobInstance := rayCronJobInstance.DeepCopy()

	// validate RayCronJob
	schedule, err := validateAndParseRayCronJob(rayCronJobInstance)
	if err != nil {
		r.Recorder.Eventf(rayCronJobInstance, corev1.EventTypeWarning, string(utils.InvalidRayCronJobSpec),
			"%s/%s: %v", rayCronJobInstance.Namespace, rayCronJobInstance.Name, err)

		rayCronJobInstance.Status.ScheduleStatus = rayv1.StatusValidationFailed

		// This is the only 2 places where we update the RayCronJob status. This will directly
		// update the ScheduleStatus to ValidationFailed if there's validation error
		if err = r.updateRayCronJobStatus(ctx, originalRayCronJobInstance, rayCronJobInstance); err != nil {
			logger.Info("Failed to update RayCronJob status", "error", err)
			return ctrl.Result{RequeueAfter: RayCronJobDefaultRequeueDuration}, err
		}
		return ctrl.Result{}, nil
	}

	now := r.clock.Now()
	scheduledTime := schedule.Next(now)
	requeueAt := scheduledTime.Sub(now)
	logger.Info("Schedule timing", "now", now, "nextScheduledTime", scheduledTime, "requeueAfter", requeueAt)

	switch rayCronJobInstance.Status.ScheduleStatus {
	case rayv1.StatusNew:
		// Update status to scheduled
		logger.Info("Status transition", "from", rayv1.StatusNew, "to", rayv1.StatusScheduled)
		rayCronJobInstance.Status.ScheduleStatus = rayv1.StatusScheduled

	case rayv1.StatusScheduled:
		rayJob := constructRayJob(rayCronJobInstance)
		if err := r.Create(ctx, rayJob); err != nil {
			logger.Error(err, "Failed to create RayJob from RayCronJob")
			// TODO: think if using requeueAt here is good?
			return ctrl.Result{RequeueAfter: requeueAt}, err
		}

		logger.Info("Successfully created RayJob", "rayJobName", rayJob.Name, "namespace", rayJob.Namespace)
		rayCronJobInstance.Status.LastScheduleTime = metav1.NewTime(now)
	default:
		logger.Info("Unknown ScheduleStatus", "ScheduleStatus", rayCronJobInstance.Status.ScheduleStatus)
		return ctrl.Result{RequeueAfter: RayCronJobDefaultRequeueDuration}, nil

	}

	// This is the only 2 places where we update the RayCronJob status. This will directly
	// update the ScheduleStatus to ValidationFailed if there's validation error
	if err = r.updateRayCronJobStatus(ctx, originalRayCronJobInstance, rayCronJobInstance); err != nil {
		logger.Info("Failed to update RayCronJob status", "error", err)
		return ctrl.Result{RequeueAfter: RayCronJobDefaultRequeueDuration}, err
	}

	return ctrl.Result{RequeueAfter: requeueAt}, nil
}

func (r *RayCronJobReconciler) updateRayCronJobStatus(ctx context.Context, oldRayCronJob *rayv1.RayCronJob, newRayCronJob *rayv1.RayCronJob) error {
	logger := ctrl.LoggerFrom(ctx)
	oldRayCronJobStatus := oldRayCronJob.Status
	newRayCronJobStatus := newRayCronJob.Status
	if oldRayCronJobStatus.ScheduleStatus != newRayCronJobStatus.ScheduleStatus {

		logger.Info("updateRayCronJobStatus", "old ScheduleStatus", oldRayCronJobStatus.ScheduleStatus, "new ScheduleStatus", newRayCronJobStatus.ScheduleStatus)
		if err := r.Status().Update(ctx, newRayCronJob); err != nil {
			return err
		}
	}
	return nil
}

// Validate the RayCronJob and return cron schedule string if valid
func validateAndParseRayCronJob(rayCronJobInstance *rayv1.RayCronJob) (cron.Schedule, error) {
	schedule, parseErr := cron.ParseStandard(rayCronJobInstance.Spec.Schedule)
	if parseErr != nil {
		// cron string validation error
		return nil, fmt.Errorf("the RayJobCron spec is invalid: Parse cron schedule with error: %w", parseErr)
	}
	if err := utils.ValidateRayJobSpec(&rayv1.RayJob{Spec: *rayCronJobInstance.Spec.JobTemplate}); err != nil {
		return nil, fmt.Errorf("the RayJobCron spec is invalid: The RayJob spec is invalid with error: %w", err)
	}

	return schedule, nil
}

func constructRayJob(cronJob *rayv1.RayCronJob) *rayv1.RayJob {
	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", cronJob.Name, rand.String(5)),
			Namespace: cronJob.Namespace,
			Labels: map[string]string{
				"ray.io/cronjob-name": cronJob.Name,
			},
		},
		Spec: *cronJob.Spec.JobTemplate.DeepCopy(),
	}
	return rayJob
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayCronJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayCronJob{}).
		Complete(r)
}
