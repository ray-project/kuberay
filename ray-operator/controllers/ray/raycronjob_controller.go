package ray

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
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

	// TODO:
	// 1. Get RayCronJob instance, set init status as New
	// 2. validate RayCronJob, if validation failed, update status to validation error
	// 3. switch RayCronJob status, New and Scheduled

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

	// validate RayCronJob
	if err := validateRayCronJob(rayCronJobInstance); err != nil {
		r.Recorder.Eventf(rayCronJobInstance, corev1.EventTypeWarning, string(utils.InvalidRayCronJobSpec),
			"%s/%s: %v", rayCronJobInstance.Namespace, rayCronJobInstance.Name, err)

		rayCronJobInstance.Status.ScheduleStatus = rayv1.StatusValidationFailed
		// TODO: directly update rayjob status
	}

	return ctrl.Result{}, nil
}

// Validate the RayCronJob
func validateRayCronJob(rayCronJobInstance *rayv1.RayCronJob) error {
	// TODO: Do we need this? Validate RayCronJob metadata -> validate the name length
	_, parseErr := cron.ParseStandard(rayCronJobInstance.Spec.Schedule)
	if parseErr != nil {
		// cron string validation error
		return fmt.Errorf("The RayJobCron spec is invalid: Parse cron schedule with error: %w", parseErr)
	}
	if err := utils.ValidateRayJobSpec(&rayv1.RayJob{Spec: *rayCronJobInstance.Spec.JobTemplate}); err != nil {
		return fmt.Errorf("The RayJobCron spec is invalid: The RayJob spec is invalid with error: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayCronJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayCronJob{}).
		Complete(r)
}
