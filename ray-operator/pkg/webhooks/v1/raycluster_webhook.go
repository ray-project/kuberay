package v1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var rayClusterLog = logf.Log.WithName("raycluster-resource")

// SetupRayClusterWebhookWithManager registers the webhook for RayCluster in the manager.
func SetupRayClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &rayv1.RayCluster{}).
		WithValidator(&RayClusterWebhook{}).
		Complete()
}

type RayClusterWebhook struct{}

//+kubebuilder:webhook:path=/validate-ray-io-v1-raycluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayclusters,verbs=create;update,versions=v1,name=vraycluster.kb.io,admissionReviewVersions=v1

var _ admission.Validator[*rayv1.RayCluster] = &RayClusterWebhook{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type
func (w *RayClusterWebhook) ValidateCreate(_ context.Context, rayCluster *rayv1.RayCluster) (admission.Warnings, error) {
	rayClusterLog.Info("validate create", "name", rayCluster.Name)
	return nil, w.validateRayCluster(rayCluster)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type
func (w *RayClusterWebhook) ValidateUpdate(_ context.Context, _ *rayv1.RayCluster, rayCluster *rayv1.RayCluster) (admission.Warnings, error) {
	rayClusterLog.Info("validate update", "name", rayCluster.Name)
	return nil, w.validateRayCluster(rayCluster)
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (w *RayClusterWebhook) ValidateDelete(_ context.Context, _ *rayv1.RayCluster) (admission.Warnings, error) {
	return nil, nil
}

func (w *RayClusterWebhook) validateRayCluster(rayCluster *rayv1.RayCluster) error {
	var allErrs field.ErrorList

	if err := utils.ValidateRayClusterMetadata(rayCluster.ObjectMeta); err != nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("name"), rayCluster.Name, err.Error()))
	}

	if err := w.validateWorkerGroups(rayCluster); err != nil {
		allErrs = append(allErrs, err)
	}

	if err := w.validateGcsFTOptions(rayCluster); err != nil {
		allErrs = append(allErrs, err)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "ray.io", Kind: "RayCluster"},
		rayCluster.Name, allErrs)
}

func (w *RayClusterWebhook) validateWorkerGroups(rayCluster *rayv1.RayCluster) *field.Error {
	workerGroupNames := make(map[string]bool)

	for i, workerGroup := range rayCluster.Spec.WorkerGroupSpecs {
		if _, ok := workerGroupNames[workerGroup.GroupName]; ok {
			return field.Invalid(field.NewPath("spec").Child("workerGroupSpecs").Index(i), workerGroup, "worker group names must be unique")
		}
		workerGroupNames[workerGroup.GroupName] = true
	}

	return nil
}

func (w *RayClusterWebhook) validateGcsFTOptions(rayCluster *rayv1.RayCluster) *field.Error {
	ftOpts := rayCluster.Spec.GcsFaultToleranceOptions
	if ftOpts == nil {
		return nil
	}

	if ftOpts.EnableActivePassiveHead == nil || !*ftOpts.EnableActivePassiveHead {
		if ftOpts.LeaderElectionLeaseDurationSeconds != nil ||
			ftOpts.LeaderElectionRenewDeadlineSeconds != nil ||
			ftOpts.LeaderElectionRetryPeriodSeconds != nil {
			return field.Invalid(
				field.NewPath("spec").Child("gcsFaultToleranceOptions"),
				ftOpts,
				"leader election lease parameters (lease duration, renew deadline, retry period) can only be configured when enableActivePassiveHead is true",
			)
		}
		return nil
	}

	if ftOpts.RedisAddress == "" {
		return field.Invalid(
			field.NewPath("spec").Child("gcsFaultToleranceOptions").Child("redisAddress"),
			ftOpts.RedisAddress,
			"redisAddress must be configured when enableActivePassiveHead is true",
		)
	}

	if (ftOpts.LeaderElectionLeaseDurationSeconds != nil && *ftOpts.LeaderElectionLeaseDurationSeconds <= 0) ||
		(ftOpts.LeaderElectionRenewDeadlineSeconds != nil && *ftOpts.LeaderElectionRenewDeadlineSeconds <= 0) ||
		(ftOpts.LeaderElectionRetryPeriodSeconds != nil && *ftOpts.LeaderElectionRetryPeriodSeconds <= 0) {
		return field.Invalid(
			field.NewPath("spec").Child("gcsFaultToleranceOptions"),
			ftOpts,
			"leader election lease parameters (lease duration, renew deadline, retry period) must be greater than 0",
		)
	}

	leaseDuration := int32(15) // default
	if ftOpts.LeaderElectionLeaseDurationSeconds != nil {
		leaseDuration = *ftOpts.LeaderElectionLeaseDurationSeconds
	}

	renewDeadline := int32(10) // default
	if ftOpts.LeaderElectionRenewDeadlineSeconds != nil {
		renewDeadline = *ftOpts.LeaderElectionRenewDeadlineSeconds
	}

	retryPeriod := int32(2) // default
	if ftOpts.LeaderElectionRetryPeriodSeconds != nil {
		retryPeriod = *ftOpts.LeaderElectionRetryPeriodSeconds
	}

	if leaseDuration <= renewDeadline {
		return field.Invalid(
			field.NewPath("spec").Child("gcsFaultToleranceOptions").Child("leaderElectionLeaseDurationSeconds"),
			leaseDuration,
			"leaderElectionLeaseDurationSeconds must be greater than leaderElectionRenewDeadlineSeconds",
		)
	}
	if renewDeadline <= retryPeriod {
		return field.Invalid(
			field.NewPath("spec").Child("gcsFaultToleranceOptions").Child("leaderElectionRenewDeadlineSeconds"),
			renewDeadline,
			"leaderElectionRenewDeadlineSeconds must be greater than leaderElectionRetryPeriodSeconds",
		)
	}

	return nil
}
