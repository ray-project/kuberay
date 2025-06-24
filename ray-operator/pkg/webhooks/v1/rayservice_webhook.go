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

var rayServiceLog = logf.Log.WithName("rayservice-resource")

// SetupRayServiceWebhookWithManager registers the webhook for RayService in the manager.
func SetupRayServiceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&rayv1.RayService{}).
		WithValidator(&RayServiceWebhook{}).
		Complete()
}

type RayServiceWebhook struct{}

//+kubebuilder:webhook:path=/validate-ray-io-v1-rayservice,mutating=false,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayservices,verbs=create;update,versions=v1,name=vrayservice.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &RayServiceWebhook{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayServiceWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	rayService := obj.(*rayv1.RayService)
	rayServiceLog.Info("validate create", "name", rayService.Name)
	return nil, w.validateRayService(rayService)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayServiceWebhook) ValidateUpdate(_ context.Context, _ runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	rayService := newObj.(*rayv1.RayService)
	rayServiceLog.Info("validate update", "name", rayService.Name)
	return nil, w.validateRayService(rayService)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayServiceWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (w *RayServiceWebhook) validateRayService(rayService *rayv1.RayService) error {
	var allErrs field.ErrorList

	if err := utils.ValidateRayServiceMetadata(rayService.ObjectMeta); err != nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("name"), rayService.Name, err.Error()))
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "ray.io", Kind: "RayService"},
		rayService.Name, allErrs)
}
