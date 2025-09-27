package utils

import (
	errstd "errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/dashboardclient"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

func ValidateRayClusterStatus(instance *rayv1.RayCluster) error {
	suspending := meta.IsStatusConditionTrue(instance.Status.Conditions, string(rayv1.RayClusterSuspending))
	suspended := meta.IsStatusConditionTrue(instance.Status.Conditions, string(rayv1.RayClusterSuspended))
	if suspending && suspended {
		return errstd.New("invalid RayCluster State: rayv1.RayClusterSuspending and rayv1.RayClusterSuspended conditions should not be both true")
	}
	return nil
}

func ValidateRayClusterMetadata(metadata metav1.ObjectMeta) error {
	if len(metadata.Name) > MaxRayClusterNameLength {
		return fmt.Errorf("RayCluster name should be no more than %d characters", MaxRayClusterNameLength)
	}
	if errs := validation.IsDNS1035Label(metadata.Name); len(errs) > 0 {
		return fmt.Errorf("RayCluster name should be a valid DNS1035 label: %v", errs)
	}
	return nil
}

// Validation for invalid Ray Cluster configurations.
func ValidateRayClusterSpec(spec *rayv1.RayClusterSpec, annotations map[string]string) error {
	if len(spec.HeadGroupSpec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("headGroupSpec should have at least one container")
	}

	for _, workerGroup := range spec.WorkerGroupSpecs {
		if len(workerGroup.Template.Spec.Containers) == 0 {
			return fmt.Errorf("workerGroupSpec should have at least one container")
		}
	}

	if annotations[RayFTEnabledAnnotationKey] != "" && spec.GcsFaultToleranceOptions != nil {
		return fmt.Errorf("%s annotation and GcsFaultToleranceOptions are both set. "+
			"Please use only GcsFaultToleranceOptions to configure GCS fault tolerance", RayFTEnabledAnnotationKey)
	}

	if !IsGCSFaultToleranceEnabled(spec, annotations) {
		if EnvVarExists(RAY_REDIS_ADDRESS, spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Env) {
			return fmt.Errorf("%s is set which implicitly enables GCS fault tolerance, "+
				"but GcsFaultToleranceOptions is not set. Please set GcsFaultToleranceOptions "+
				"to enable GCS fault tolerance", RAY_REDIS_ADDRESS)
		}
	}

	headContainer := spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex]
	if spec.GcsFaultToleranceOptions != nil {
		if redisPassword := spec.HeadGroupSpec.RayStartParams["redis-password"]; redisPassword != "" {
			return fmt.Errorf("cannot set `redis-password` in rayStartParams when " +
				"GcsFaultToleranceOptions is enabled - use GcsFaultToleranceOptions.RedisPassword instead")
		}

		if EnvVarExists(REDIS_PASSWORD, headContainer.Env) {
			return fmt.Errorf("cannot set `REDIS_PASSWORD` env var in head Pod when " +
				"GcsFaultToleranceOptions is enabled - use GcsFaultToleranceOptions.RedisPassword instead")
		}

		if EnvVarExists(RAY_REDIS_ADDRESS, headContainer.Env) {
			return fmt.Errorf("cannot set `RAY_REDIS_ADDRESS` env var in head Pod when " +
				"GcsFaultToleranceOptions is enabled - use GcsFaultToleranceOptions.RedisAddress instead")
		}

		if annotations[RayExternalStorageNSAnnotationKey] != "" {
			return fmt.Errorf("cannot set `ray.io/external-storage-namespace` annotation when " +
				"GcsFaultToleranceOptions is enabled - use GcsFaultToleranceOptions.ExternalStorageNamespace instead")
		}
	}
	if spec.HeadGroupSpec.RayStartParams["redis-username"] != "" || EnvVarExists(REDIS_USERNAME, headContainer.Env) {
		return fmt.Errorf("cannot set redis username in rayStartParams or environment variables" +
			" - use GcsFaultToleranceOptions.RedisUsername instead")
	}

	if !features.Enabled(features.RayJobDeletionPolicy) {
		for _, workerGroup := range spec.WorkerGroupSpecs {
			if workerGroup.Suspend != nil && *workerGroup.Suspend {
				return fmt.Errorf("worker group %s can be suspended only when the RayJobDeletionPolicy feature gate is enabled", workerGroup.GroupName)
			}
		}
	}

	// Check if autoscaling is enabled once to avoid repeated calls
	isAutoscalingEnabled := IsAutoscalingEnabled(spec)

	// Validate that RAY_enable_autoscaler_v2 environment variable is not set to "1" or "true" when autoscaler is disabled
	if !isAutoscalingEnabled {
		if envVar, exists := EnvVarByName(RAY_ENABLE_AUTOSCALER_V2, spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Env); exists {
			if envVar.Value == "1" || envVar.Value == "true" {
				return fmt.Errorf("environment variable %s cannot be set to '%s' when enableInTreeAutoscaling is false. Please set enableInTreeAutoscaling: true to use autoscaler v2", RAY_ENABLE_AUTOSCALER_V2, envVar.Value)
			}
		}
	}

	if isAutoscalingEnabled {
		for _, workerGroup := range spec.WorkerGroupSpecs {
			if workerGroup.Suspend != nil && *workerGroup.Suspend {
				// TODO (rueian): This can be supported in future Ray. We should check the RayVersion once we know the version.
				return fmt.Errorf("worker group %s cannot be suspended with Autoscaler enabled", workerGroup.GroupName)
			}
		}

		if spec.AutoscalerOptions != nil && spec.AutoscalerOptions.Version != nil && EnvVarExists(RAY_ENABLE_AUTOSCALER_V2, spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Env) {
			return fmt.Errorf("both .spec.autoscalerOptions.version and head Pod env var %s are set, please only use the former", RAY_ENABLE_AUTOSCALER_V2)
		}

		if IsAutoscalingV2Enabled(spec) {
			if spec.HeadGroupSpec.Template.Spec.RestartPolicy != "" && spec.HeadGroupSpec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
				return fmt.Errorf("restartPolicy for head Pod should be Never or unset when using autoscaler V2")
			}

			for _, workerGroup := range spec.WorkerGroupSpecs {
				if workerGroup.Template.Spec.RestartPolicy != "" && workerGroup.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
					return fmt.Errorf("restartPolicy for worker group %s should be Never or unset when using autoscaler V2", workerGroup.GroupName)
				}
			}
		}
	}
	return nil
}

func ValidateRayJobStatus(rayJob *rayv1.RayJob) error {
	if rayJob.Status.JobDeploymentStatus == rayv1.JobDeploymentStatusWaiting && rayJob.Spec.SubmissionMode != rayv1.InteractiveMode {
		return fmt.Errorf("The RayJob status is invalid: JobDeploymentStatus cannot be `Waiting` when SubmissionMode is not InteractiveMode")
	}
	return nil
}

func ValidateRayJobMetadata(metadata metav1.ObjectMeta) error {
	if len(metadata.Name) > MaxRayJobNameLength {
		return fmt.Errorf("The RayJob metadata is invalid: RayJob name should be no more than %d characters", MaxRayJobNameLength)
	}
	if errs := validation.IsDNS1035Label(metadata.Name); len(errs) > 0 {
		return fmt.Errorf("The RayJob metadata is invalid: RayJob name should be a valid DNS1035 label: %v", errs)
	}
	return nil
}

func ValidateRayJobSpec(rayJob *rayv1.RayJob) error {
	// KubeRay has some limitations for the suspend operation. The limitations are a subset of the limitations of
	// Kueue (https://kueue.sigs.k8s.io/docs/tasks/run_rayjobs/#c-limitations). For example, KubeRay allows users
	// to suspend a RayJob with autoscaling enabled, but Kueue doesn't.
	if rayJob.Spec.Suspend && !rayJob.Spec.ShutdownAfterJobFinishes {
		return fmt.Errorf("The RayJob spec is invalid: a RayJob with shutdownAfterJobFinishes set to false is not allowed to be suspended")
	}

	if rayJob.Spec.TTLSecondsAfterFinished < 0 {
		return fmt.Errorf("The RayJob spec is invalid: TTLSecondsAfterFinished must be a non-negative integer")
	}

	// Validate TTL and deletion strategy together
	if err := validateDeletionConfiguration(rayJob); err != nil {
		return err
	}

	isClusterSelectorMode := len(rayJob.Spec.ClusterSelector) != 0
	if rayJob.Spec.Suspend && isClusterSelectorMode {
		return fmt.Errorf("The RayJob spec is invalid: the ClusterSelector mode doesn't support the suspend operation")
	}
	if rayJob.Spec.RayClusterSpec == nil && !isClusterSelectorMode {
		return fmt.Errorf("The RayJob spec is invalid: one of RayClusterSpec or ClusterSelector must be set")
	}
	if isClusterSelectorMode {
		clusterName := rayJob.Spec.ClusterSelector[RayJobClusterSelectorKey]
		if len(clusterName) == 0 {
			return fmt.Errorf("cluster name in ClusterSelector should not be empty")
		}
		if rayJob.Spec.SubmissionMode == rayv1.SidecarMode {
			return fmt.Errorf("ClusterSelector is not supported in SidecarMode")
		}
	}

	// InteractiveMode does not support backoffLimit > 1.
	// When a RayJob fails (e.g., due to a missing script) and retries,
	// spec.JobId remains set, causing the new job to incorrectly transition
	// to Running instead of Waiting or Failed.
	// After discussion, we decided to disallow retries in InteractiveMode
	// to avoid ambiguous state handling and unintended behavior.
	// https://github.com/ray-project/kuberay/issues/3525
	if rayJob.Spec.SubmissionMode == rayv1.InteractiveMode && rayJob.Spec.BackoffLimit != nil && *rayJob.Spec.BackoffLimit > 0 {
		return fmt.Errorf("The RayJob spec is invalid: BackoffLimit is incompatible with InteractiveMode")
	}

	if rayJob.Spec.SubmissionMode == rayv1.SidecarMode {
		if rayJob.Spec.SubmitterPodTemplate != nil {
			return fmt.Errorf("Currently, SidecarMode doesn't support SubmitterPodTemplate")
		}

		if rayJob.Spec.SubmitterConfig != nil {
			return fmt.Errorf("Currently, SidecarMode doesn't support SubmitterConfig")
		}

		if rayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.RestartPolicy != "" && rayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
			return fmt.Errorf("restartPolicy for head Pod should be Never or unset when using SidecarMode")
		}
	}

	if rayJob.Spec.RayClusterSpec != nil {
		if err := ValidateRayClusterSpec(rayJob.Spec.RayClusterSpec, rayJob.Annotations); err != nil {
			return fmt.Errorf("The RayJob spec is invalid: %w", err)
		}
	}

	// Validate whether RuntimeEnvYAML is a valid YAML string. Note that this only checks its validity
	// as a YAML string, not its adherence to the runtime environment schema.
	if _, err := dashboardclient.UnmarshalRuntimeEnvYAML(rayJob.Spec.RuntimeEnvYAML); err != nil {
		return err
	}
	if rayJob.Spec.ActiveDeadlineSeconds != nil && *rayJob.Spec.ActiveDeadlineSeconds <= 0 {
		return fmt.Errorf("The RayJob spec is invalid: activeDeadlineSeconds must be a positive integer")
	}
	if rayJob.Spec.BackoffLimit != nil && *rayJob.Spec.BackoffLimit < 0 {
		return fmt.Errorf("The RayJob spec is invalid: backoffLimit must be a positive integer")
	}

	return nil
}

func ValidateRayServiceMetadata(metadata metav1.ObjectMeta) error {
	if len(metadata.Name) > MaxRayServiceNameLength {
		return fmt.Errorf("RayService name should be no more than %d characters", MaxRayServiceNameLength)
	}
	if errs := validation.IsDNS1035Label(metadata.Name); len(errs) > 0 {
		return fmt.Errorf("RayService name should be a valid DNS1035 label: %v", errs)
	}
	return nil
}

func ValidateRayServiceSpec(rayService *rayv1.RayService) error {
	if err := ValidateRayClusterSpec(&rayService.Spec.RayClusterSpec, rayService.Annotations); err != nil {
		return err
	}

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

	if rayService.Spec.RayClusterDeletionDelaySeconds != nil &&
		*rayService.Spec.RayClusterDeletionDelaySeconds < 0 {
		return fmt.Errorf("Spec.RayClusterDeletionDelaySeconds should be a non-negative integer, got %d", *rayService.Spec.RayClusterDeletionDelaySeconds)
	}

	return nil
}

// validateDeletionConfiguration validates both deletion strategy and TTL configuration
func validateDeletionConfiguration(rayJob *rayv1.RayJob) error {
	if !rayJob.Spec.ShutdownAfterJobFinishes && rayJob.Spec.TTLSecondsAfterFinished > 0 {
		return fmt.Errorf("The RayJob spec is invalid: a RayJob with shutdownAfterJobFinishes set to false cannot have TTLSecondsAfterFinished")
	}

	// No strategy block: nothing else to validate.
	if rayJob.Spec.DeletionStrategy == nil {
		return nil
	}

	// Feature gate must be enabled for any strategy usage.
	if !features.Enabled(features.RayJobDeletionPolicy) {
		return fmt.Errorf("RayJobDeletionPolicy feature gate must be enabled to use DeletionStrategy")
	}

	legacyConfigured := rayJob.Spec.DeletionStrategy.OnSuccess != nil || rayJob.Spec.DeletionStrategy.OnFailure != nil
	rulesConfigured := rayJob.Spec.DeletionStrategy.DeletionRules != nil // explicit empty slice counts as rules mode

	// Mutual exclusivity: rules mode forbids shutdown & legacy. (TTL+rules is implicitly invalid because TTL requires shutdown.)
	if rulesConfigured && rayJob.Spec.ShutdownAfterJobFinishes {
		return fmt.Errorf("The RayJob spec is invalid: spec.shutdownAfterJobFinishes and spec.deletionStrategy.deletionRules are mutually exclusive")
	}
	if rulesConfigured && legacyConfigured {
		return fmt.Errorf("The RayJob spec is invalid: Cannot use both legacy onSuccess/onFailure fields and deletionRules simultaneously")
	}

	// Detailed content validation
	if legacyConfigured {
		if err := validateLegacyDeletionPolicies(rayJob); err != nil {
			return err
		}
	} else if rulesConfigured {
		if err := validateDeletionRules(rayJob); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("The RayJob spec is invalid: DeletionStrategy requires either BOTH onSuccess and onFailure, OR the deletionRules field (which may be empty list)")
	}

	return nil
}

// validateDeletionRules validates the deletion rules in the RayJob spec.
// It performs per-rule validations, checks for uniqueness, and ensures logical TTL consistency.
// Errors are collected and returned as a single aggregated error using errors.Join for better user feedback.
func validateDeletionRules(rayJob *rayv1.RayJob) error {
	rules := rayJob.Spec.DeletionStrategy.DeletionRules
	isClusterSelectorMode := len(rayJob.Spec.ClusterSelector) != 0

	// Group TTLs by JobStatus for cross-rule validation and uniqueness checking.
	rulesByStatus := make(map[rayv1.JobStatus]map[rayv1.DeletionPolicyType]int32)
	var errs []error

	// Single pass: Validate each rule individually and group for later consistency checks.
	for i, rule := range rules {
		// Validate TTL is non-negative.
		if rule.Condition.TTLSeconds < 0 {
			errs = append(errs, fmt.Errorf("deletionRules[%d]: TTLSeconds must be non-negative", i))
			continue
		}

		// Contextual validations based on spec.
		if isClusterSelectorMode && (rule.Policy == rayv1.DeleteCluster || rule.Policy == rayv1.DeleteWorkers) {
			errs = append(errs, fmt.Errorf("deletionRules[%d]: DeletionPolicyType '%s' not supported when ClusterSelector is set", i, rule.Policy))
			continue
		}
		if IsAutoscalingEnabled(rayJob.Spec.RayClusterSpec) && rule.Policy == rayv1.DeleteWorkers {
			// TODO (rueian): Support in future Ray versions by checking RayVersion.
			errs = append(errs, fmt.Errorf("deletionRules[%d]: DeletionPolicyType 'DeleteWorkers' not supported with autoscaling enabled", i))
			continue
		}

		// Group valid rule for consistency check.
		policyTTLs, ok := rulesByStatus[rule.Condition.JobStatus]
		if !ok {
			policyTTLs = make(map[rayv1.DeletionPolicyType]int32)
			rulesByStatus[rule.Condition.JobStatus] = policyTTLs
		}

		// Check for uniqueness of (JobStatus, DeletionPolicyType) pair.
		if _, exists := policyTTLs[rule.Policy]; exists {
			errs = append(errs, fmt.Errorf("deletionRules[%d]: duplicate rule for DeletionPolicyType '%s' and JobStatus '%s'", i, rule.Policy, rule.Condition.JobStatus))
			continue
		}

		policyTTLs[rule.Policy] = rule.Condition.TTLSeconds
	}

	// Second pass: Validate TTL consistency per JobStatus.
	for status, policyTTLs := range rulesByStatus {
		if err := validateTTLConsistency(policyTTLs, status); err != nil {
			errs = append(errs, err)
		}
	}

	return errstd.Join(errs...)
}

// validateTTLConsistency ensures TTLs follow the deletion hierarchy: Workers <= Cluster <= Self.
// (Lower TTL means deletes earlier.)
func validateTTLConsistency(policyTTLs map[rayv1.DeletionPolicyType]int32, status rayv1.JobStatus) error {
	// Define the required deletion order. TTLs must be non-decreasing along this sequence.
	deletionOrder := []rayv1.DeletionPolicyType{
		rayv1.DeleteWorkers,
		rayv1.DeleteCluster,
		rayv1.DeleteSelf,
	}

	var prevPolicy rayv1.DeletionPolicyType
	var prevTTL int32
	var hasPrev bool

	var errs []error

	for _, policy := range deletionOrder {
		ttl, exists := policyTTLs[policy]
		if !exists {
			continue
		}

		if hasPrev && ttl < prevTTL {
			errs = append(errs, fmt.Errorf(
				"for JobStatus '%s': %s TTL (%d) must be >= %s TTL (%d)",
				status, policy, ttl, prevPolicy, prevTTL,
			))
		}

		prevPolicy = policy
		prevTTL = ttl
		hasPrev = true
	}

	return errstd.Join(errs...)
}

// validateLegacyDeletionPolicies handles validation for the old `onSuccess` and `onFailure` fields.
func validateLegacyDeletionPolicies(rayJob *rayv1.RayJob) error {
	isClusterSelectorMode := len(rayJob.Spec.ClusterSelector) != 0

	// Both policies must be set if using the legacy API.
	if rayJob.Spec.DeletionStrategy.OnSuccess == nil || rayJob.Spec.DeletionStrategy.OnFailure == nil {
		return fmt.Errorf("both DeletionStrategy.OnSuccess and DeletionStrategy.OnFailure must be set when using the legacy deletion policy fields of DeletionStrategy")
	}

	// Validate that the Policy field is set within each policy.
	onSuccessPolicy := rayJob.Spec.DeletionStrategy.OnSuccess
	onFailurePolicy := rayJob.Spec.DeletionStrategy.OnFailure

	if onSuccessPolicy.Policy == nil {
		return fmt.Errorf("the DeletionPolicyType field of DeletionStrategy.OnSuccess cannot be unset when DeletionStrategy is enabled")
	}
	if onFailurePolicy.Policy == nil {
		return fmt.Errorf("the DeletionPolicyType field of DeletionStrategy.OnFailure cannot be unset when DeletionStrategy is enabled")
	}

	if isClusterSelectorMode {
		if *onSuccessPolicy.Policy == rayv1.DeleteCluster || *onSuccessPolicy.Policy == rayv1.DeleteWorkers {
			return fmt.Errorf("the ClusterSelector mode doesn't support DeletionStrategy=%s on success", *onSuccessPolicy.Policy)
		}
		if *onFailurePolicy.Policy == rayv1.DeleteCluster || *onFailurePolicy.Policy == rayv1.DeleteWorkers {
			return fmt.Errorf("the ClusterSelector mode doesn't support DeletionStrategy=%s on failure", *onFailurePolicy.Policy)
		}
	}

	if (*onSuccessPolicy.Policy == rayv1.DeleteWorkers || *onFailurePolicy.Policy == rayv1.DeleteWorkers) && IsAutoscalingEnabled(rayJob.Spec.RayClusterSpec) {
		// TODO (rueian): This can be supported in a future Ray version. We should check the RayVersion once we know it.
		return fmt.Errorf("DeletionStrategy=DeleteWorkers currently does not support RayCluster with autoscaling enabled")
	}

	if rayJob.Spec.ShutdownAfterJobFinishes && (*onSuccessPolicy.Policy == rayv1.DeleteNone || *onFailurePolicy.Policy == rayv1.DeleteNone) {
		return fmt.Errorf("The RayJob spec is invalid: shutdownAfterJobFinshes is set to 'true' while deletion policy is 'DeleteNone'")
	}

	return nil
}
