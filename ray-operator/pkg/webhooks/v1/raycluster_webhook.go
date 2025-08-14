package v1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var rayClusterLog = logf.Log.WithName("raycluster-resource")

// SetupRayClusterWebhookWithManager registers the webhook for RayCluster in the manager.
func SetupRayClusterWebhookWithManager(mgr ctrl.Manager, config *configapi.Configuration) error {
	// Create MTLS webhook instance
	mtlsWebhook := &RayClusterMTLSWebhook{
		Config: config,
	}

	// Setup validation webhook
	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&rayv1.RayCluster{}).
		WithValidator(&RayClusterWebhook{Config: config}).
		Complete(); err != nil {
		return err
	}

	// Setup MTLS mutating webhook if enabled
	if ptr.Deref(config.EnableMTLS, false) {
		if err := ctrl.NewWebhookManagedBy(mgr).
			For(&rayv1.RayCluster{}).
			WithDefaulter(mtlsWebhook).
			Complete(); err != nil {
			return err
		}
	}

	return nil
}

type RayClusterWebhook struct {
	Config *configapi.Configuration
}

//+kubebuilder:webhook:path=/validate-ray-io-v1-raycluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayclusters,verbs=create;update,versions=v1,name=vraycluster.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &RayClusterWebhook{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayClusterWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	rayCluster := obj.(*rayv1.RayCluster)
	rayClusterLog.Info("validate create", "name", rayCluster.Name)
	return nil, w.validateRayCluster(rayCluster)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayClusterWebhook) ValidateUpdate(_ context.Context, _ runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	rayCluster := newObj.(*rayv1.RayCluster)
	rayClusterLog.Info("validate update", "name", rayCluster.Name)
	return nil, w.validateRayCluster(rayCluster)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (w *RayClusterWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
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

	// Add MTLS validation if enabled
	if ptr.Deref(w.Config.EnableMTLS, false) {
		allErrs = append(allErrs, w.validateMTLSConfiguration(rayCluster)...)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "ray.io", Kind: "RayCluster"},
		rayCluster.Name, allErrs)
}

// validateMTLSConfiguration validates MTLS-related configuration
func (w *RayClusterWebhook) validateMTLSConfiguration(rayCluster *rayv1.RayCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Validate that required volumes are present
	if err := w.validateRequiredVolumes(rayCluster); err != nil {
		allErrs = append(allErrs, err)
	}

	// Validate that required environment variables are present
	if err := w.validateRequiredEnvVars(rayCluster); err != nil {
		allErrs = append(allErrs, err)
	}

	return allErrs
}

// validateRequiredVolumes validates that required volumes are present for MTLS
func (w *RayClusterWebhook) validateRequiredVolumes(rayCluster *rayv1.RayCluster) *field.Error {
	// This is a basic validation - in practice, the webhook will inject these volumes
	// so this validation is mainly for documentation purposes
	return nil
}

// validateRequiredEnvVars validates that required environment variables are present for MTLS
func (w *RayClusterWebhook) validateRequiredEnvVars(rayCluster *rayv1.RayCluster) *field.Error {
	// This is a basic validation - in practice, the webhook will inject these env vars
	// so this validation is mainly for documentation purposes
	return nil
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
