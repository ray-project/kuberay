package v1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var rayJobLog = logf.Log.WithName("rayjob-resource")

// SetupRayJobWebhookWithManager registers the webhook for RayJob in the manager.
func SetupRayJobWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&rayv1.RayJob{}).
		WithValidator(&RayJobWebhook{}).
		Complete()
}

type RayJobWebhook struct{}

//+kubebuilder:webhook:path=/validate-ray-io-v1-rayjob,mutating=false,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayjobs,verbs=create;update,versions=v1,name=vrayjob.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &RayJobWebhook{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayJobWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	rayJob := obj.(*rayv1.RayJob)
	rayJobLog.Info("validate create", "name", rayJob.Name)
	return nil, w.validateRayJob(rayJob)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayJobWebhook) ValidateUpdate(_ context.Context, _ runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	rayJob := newObj.(*rayv1.RayJob)
	rayJobLog.Info("validate update", "name", rayJob.Name)
	return nil, w.validateRayJob(rayJob)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayJobWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (w *RayJobWebhook) validateRayJob(rayJob *rayv1.RayJob) error {
	var allErrs field.ErrorList

	if err := utils.ValidateRayJobMetadata(rayJob.ObjectMeta); err != nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("name"), rayJob.Name, err.Error()))
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "ray.io", Kind: "RayJob"},
		rayJob.Name, allErrs)
}
