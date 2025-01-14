package v1

import (
	"fmt"
	"regexp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var (
	rayclusterlog = logf.Log.WithName("raycluster-resource")
	nameRegex, _  = regexp.Compile("^[a-z]([-a-z0-9]*[a-z0-9])?$")
)

func (r *RayCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-ray-io-v1-raycluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayclusters,verbs=create;update,versions=v1,name=vraycluster.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &RayCluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *RayCluster) ValidateCreate() (admission.Warnings, error) {
	rayclusterlog.Info("validate create", "name", r.Name)
	return nil, r.validateRayCluster()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *RayCluster) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	rayclusterlog.Info("validate update", "name", r.Name)
	return nil, r.validateRayCluster()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *RayCluster) ValidateDelete() (admission.Warnings, error) {
	rayclusterlog.Info("validate delete", "name", r.Name)
	return nil, nil
}

func (r *RayCluster) validateRayCluster() error {
	var allErrs field.ErrorList

	if err := r.validateName(); err != nil {
		allErrs = append(allErrs, err)
	}

	if err := r.validateWorkerGroups(); err != nil {
		allErrs = append(allErrs, err)
	}

	if err := r.ValidateRayClusterSpec(); err != nil {
		allErrs = append(allErrs, err)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "ray.io", Kind: "RayCluster"},
		r.Name, allErrs)
}

func (r *RayCluster) validateName() *field.Error {
	if !nameRegex.MatchString(r.Name) {
		return field.Invalid(field.NewPath("metadata").Child("name"), r.Name, "name must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')")
	}
	return nil
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

func (r *RayCluster) ValidateRayClusterSpec() *field.Error {
	if r.Annotations[RayFTEnabledAnnotationKey] == "false" && r.Spec.GcsFaultToleranceOptions != nil {
		return field.Invalid(
			field.NewPath("spec").Child("gcsFaultToleranceOptions"),
			r.Spec.GcsFaultToleranceOptions,
			fmt.Sprintf("GcsFaultToleranceOptions should be nil when %s annotation is set to false", RayFTEnabledAnnotationKey),
		)
	}
	if r.Annotations[RayFTEnabledAnnotationKey] != "true" && len(r.Spec.HeadGroupSpec.Template.Spec.Containers) > 0 {
		if EnvVarExists(RAY_REDIS_ADDRESS, r.Spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Env) {
			return field.Invalid(
				field.NewPath("spec").Child("headGroupSpec").Child("template").Child("spec").Child("containers").Index(0).Child("env"),
				RAY_REDIS_ADDRESS,
				fmt.Sprintf("%s should not be set when %s is disabled", RAY_REDIS_ADDRESS, RayFTEnabledAnnotationKey),
			)
		}
	}
	return nil
}
