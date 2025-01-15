package utils

import (
	errstd "errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func ValidateRayClusterStatus(instance *rayv1.RayCluster) error {
	suspending := meta.IsStatusConditionTrue(instance.Status.Conditions, string(rayv1.RayClusterSuspending))
	suspended := meta.IsStatusConditionTrue(instance.Status.Conditions, string(rayv1.RayClusterSuspended))
	if suspending && suspended {
		return errstd.New("invalid RayCluster State: rayv1.RayClusterSuspending and rayv1.RayClusterSuspended conditions should not be both true")
	}
	return nil
}

func ValidateRayJobSpec(rayJob *rayv1.RayJob) error {
	// KubeRay has some limitations for the suspend operation. The limitations are a subset of the limitations of
	// Kueue (https://kueue.sigs.k8s.io/docs/tasks/run_rayjobs/#c-limitations). For example, KubeRay allows users
	// to suspend a RayJob with autoscaling enabled, but Kueue doesn't.
	if rayJob.Spec.Suspend && !rayJob.Spec.ShutdownAfterJobFinishes {
		return fmt.Errorf("a RayJob with shutdownAfterJobFinishes set to false is not allowed to be suspended")
	}
	if rayJob.Spec.Suspend && len(rayJob.Spec.ClusterSelector) != 0 {
		return fmt.Errorf("the ClusterSelector mode doesn't support the suspend operation")
	}
	if rayJob.Spec.RayClusterSpec == nil && len(rayJob.Spec.ClusterSelector) == 0 {
		return fmt.Errorf("one of RayClusterSpec or ClusterSelector must be set")
	}
	// Validate whether RuntimeEnvYAML is a valid YAML string. Note that this only checks its validity
	// as a YAML string, not its adherence to the runtime environment schema.
	if _, err := UnmarshalRuntimeEnvYAML(rayJob.Spec.RuntimeEnvYAML); err != nil {
		return err
	}
	if rayJob.Spec.ActiveDeadlineSeconds != nil && *rayJob.Spec.ActiveDeadlineSeconds <= 0 {
		return fmt.Errorf("activeDeadlineSeconds must be a positive integer")
	}
	if rayJob.Spec.BackoffLimit != nil && *rayJob.Spec.BackoffLimit < 0 {
		return fmt.Errorf("backoffLimit must be a positive integer")
	}
	if rayJob.Spec.ShutdownAfterJobFinishes && rayJob.Spec.DeletionPolicy != nil && *rayJob.Spec.DeletionPolicy == rayv1.DeleteNoneDeletionPolicy {
		return fmt.Errorf("shutdownAfterJobFinshes is set to 'true' while deletion policy is 'DeleteNone'")
	}
	return nil
}

func ValidateRayJobStatus(rayJob *rayv1.RayJob) error {
	if rayJob.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusWaiting && rayJob.Spec.SubmissionMode != rayv1.InteractiveMode {
		return fmt.Errorf("invalid RayJob State: JobDeploymentStatus cannot be `Waiting` when SubmissionMode is not InteractiveMode")
	}
	return nil
}

func ValidateRayServiceSpec(rayService *rayv1.RayService) error {
	if headSvc := rayService.Spec.RayClusterSpec.HeadGroupSpec.HeadService; headSvc != nil && headSvc.Name != "" {
		return fmt.Errorf("spec.rayClusterConfig.headGroupSpec.headService.metadata.name should not be set")
	}

	// only NewCluster and None are valid upgradeType
	if rayService.Spec.UpgradeStrategy != nil &&
		rayService.Spec.UpgradeStrategy.Type != nil &&
		*rayService.Spec.UpgradeStrategy.Type != rayv1.None &&
		*rayService.Spec.UpgradeStrategy.Type != rayv1.NewCluster {
		return fmt.Errorf("Spec.UpgradeStrategy.Type value %s is invalid, valid options are %s or %s", *rayService.Spec.UpgradeStrategy.Type, rayv1.NewCluster, rayv1.None)
	}
	return nil
}
