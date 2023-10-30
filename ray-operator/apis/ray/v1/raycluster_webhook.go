package v1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var rayclusterlog = logf.Log.WithName("raycluster-resource")

func (r *RayCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-ray-io-v1-raycluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayclusters,verbs=create;update,versions=v1,name=mraycluster.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &RayCluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *RayCluster) Default() {
	rayclusterlog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-ray-io-v1-raycluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayclusters,verbs=create;update,versions=v1,name=vraycluster.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &RayCluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *RayCluster) ValidateCreate() error {
	rayclusterlog.Info("validate create", "name", r.Name)
	return r.validateRayCluster()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *RayCluster) ValidateUpdate(old runtime.Object) error {
	rayclusterlog.Info("validate update", "name", r.Name)
	return r.validateRayCluster()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *RayCluster) ValidateDelete() error {
	rayclusterlog.Info("validate delete", "name", r.Name)
	return nil
}

func (r *RayCluster) validateRayCluster() error {
	var allErrs field.ErrorList
	if err := r.validateWorkerGroups(); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "ray.io", Kind: "RayCluster"},
		r.Name, allErrs)
}

func (r *RayCluster) validateWorkerGroups() *field.Error {
	workerGroupNames := make(map[string]bool)

	for i, workerGroup := range r.Spec.WorkerGroupSpecs {
		if _, ok := workerGroupNames[workerGroup.GroupName]; ok {
			return field.Invalid(field.NewPath("spec").Child("workerGroupSpecs").Index(i), workerGroup, "worker group names must be unique")
		}
		workerGroupNames[workerGroup.GroupName] = true
	}

	return nil
}
