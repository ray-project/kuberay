package v1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
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

	if err := w.validateGracefulTermination(rayCluster); err != nil {
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

// validateGracefulTermination rejects a drainDeadlineSeconds that can never
// be honored: kubelet kills the preStop hook, and the container right after
// it, once a Pod's terminationGracePeriodSeconds elapses, regardless of
// where the drain loop the hook runs is. A drainDeadlineSeconds beyond the
// effective grace period is therefore always a misconfiguration, not a
// choice - the Pod builder (common.configureGracefulTermination) also
// clamps it defensively, but rejecting it here catches the mistake at
// apply time instead of silently capping it at runtime.
func (w *RayClusterWebhook) validateGracefulTermination(rayCluster *rayv1.RayCluster) *field.Error {
	if !utils.IsGracefulTerminationEnabled(&rayCluster.Spec, rayCluster.Annotations) {
		return nil
	}
	opts := rayCluster.Spec.GracefulTerminationOptions

	checkGroup := func(podSpec *corev1.PodSpec, path *field.Path) *field.Error {
		gracePeriodSeconds, drainDeadlineSeconds := utils.ResolveGracefulTerminationSeconds(podSpec, opts)
		if drainDeadlineSeconds > gracePeriodSeconds {
			return field.Invalid(path, drainDeadlineSeconds, fmt.Sprintf(
				"gracefulTerminationOptions.drainDeadlineSeconds (%d) must not exceed the effective terminationGracePeriodSeconds (%d); kubelet kills the preStop hook once the grace period elapses regardless of drain progress",
				drainDeadlineSeconds, gracePeriodSeconds))
		}
		return nil
	}

	if err := checkGroup(&rayCluster.Spec.HeadGroupSpec.Template.Spec, field.NewPath("spec").Child("headGroupSpec").Child("template").Child("spec").Child("terminationGracePeriodSeconds")); err != nil {
		return err
	}
	for i, workerGroup := range rayCluster.Spec.WorkerGroupSpecs {
		if err := checkGroup(&workerGroup.Template.Spec, field.NewPath("spec").Child("workerGroupSpecs").Index(i).Child("template").Child("spec").Child("terminationGracePeriodSeconds")); err != nil {
			return err
		}
	}

	return nil
}
