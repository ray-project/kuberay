package common

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	pkgutils "github.com/ray-project/kuberay/ray-operator/pkg/utils"
)

// BuildHeadServiceForRayJob builds the service for a pod. Currently, there is only one service that allows
// the worker nodes to connect to the head node.
// RayJob controller updates the service whenever a new RayCluster serves the traffic.
func BuildHeadServiceForRayJob(ctx context.Context, rayJob rayv1.RayJob, rayCluster rayv1.RayCluster) (*corev1.Service, error) {
	service, err := BuildServiceForHeadPod(ctx, rayCluster, nil, nil)
	if err != nil {
		return nil, err
	}

	headSvcName, err := utils.GenerateHeadServiceName(utils.RayJobCRD, rayv1.RayClusterSpec{}, rayJob.Name)
	if err != nil {
		return nil, err
	}

	service.ObjectMeta.Name = headSvcName
	service.ObjectMeta.Namespace = rayJob.Namespace
	service.ObjectMeta.Labels = map[string]string{
		utils.RayOriginatedFromCRNameLabelKey: rayJob.Name,
		utils.RayOriginatedFromCRDLabelKey:    utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD),
		utils.RayNodeTypeLabelKey:             string(rayv1.HeadNode),
		utils.RayIDLabelKey:                   utils.CheckLabel(utils.GenerateIdentifier(rayJob.Name, rayv1.HeadNode)),
	}

	return service, nil
}

// GetRuntimeEnvJson returns the JSON string of the runtime environment for the Ray job.
func getRuntimeEnvJson(rayJobInstance *rayv1.RayJob) (string, error) {
	runtimeEnvYAML := rayJobInstance.Spec.RuntimeEnvYAML

	if len(runtimeEnvYAML) > 0 {
		// Convert YAML to JSON
		jsonData, err := yaml.YAMLToJSON(pkgutils.ConvertStringToByteSlice(runtimeEnvYAML))
		if err != nil {
			return "", err
		}
		// We return the JSON as a string
		return pkgutils.ConvertByteSliceToString(jsonData), nil
	}

	return "", nil
}

// GetMetadataJson returns the JSON string of the metadata for the Ray job.
func GetMetadataJson(metadata map[string]string, rayVersion string) (string, error) {
	// Check that the Ray version is at least 2.6.0.
	// If it is, we can use the --metadata-json flag.
	// Otherwise, we need to raise an error.
	constraint, _ := semver.NewConstraint(">= 2.6.0")
	v, err := semver.NewVersion(rayVersion)
	if err != nil {
		return "", fmt.Errorf("failed to parse Ray version: %v: %w", rayVersion, err)
	}
	if !constraint.Check(v) {
		return "", fmt.Errorf("the Ray version must be at least 2.6.0 to use the metadata field")
	}
	// Convert the metadata map to a JSON string.
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata: %v: %w", metadata, err)
	}
	return pkgutils.ConvertByteSliceToString(metadataBytes), nil
}

// BuildJobSubmitCommand builds the `ray job submit` command based on submission mode.
func BuildJobSubmitCommand(rayJobInstance *rayv1.RayJob, submissionMode rayv1.JobSubmissionMode) ([]string, error) {
	var address string
	port := utils.DefaultDashboardPort

	switch submissionMode {
	case rayv1.SidecarMode:
		// The sidecar submitter shares the same network namespace as the Ray dashboard,
		// so it uses 127.0.0.1 to connect to the Ray dashboard.
		rayHeadContainer := rayJobInstance.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex]
		port = utils.FindContainerPort(&rayHeadContainer, utils.DashboardPortName, utils.DefaultDashboardPort)
		address = "http://127.0.0.1:" + strconv.Itoa(port)
	case rayv1.K8sJobMode:
		// Submitter is a separate K8s Job; use cluster dashboard address.
		address = rayJobInstance.Status.DashboardURL
		if !strings.HasPrefix(address, "http://") {
			address = "http://" + address
		}
	default:
		return nil, fmt.Errorf("unsupported submission mode for job submit command: %s", submissionMode)
	}

	var cmd []string
	metadata := rayJobInstance.Spec.Metadata
	jobId := rayJobInstance.Status.JobId
	entrypoint := strings.TrimSpace(rayJobInstance.Spec.Entrypoint)
	entrypointNumCpus := rayJobInstance.Spec.EntrypointNumCpus
	entrypointNumGpus := rayJobInstance.Spec.EntrypointNumGpus
	entrypointResources := rayJobInstance.Spec.EntrypointResources

	// In K8sJobMode, we need to avoid submitting the job twice, since the job submitter might retry.
	// `ray job submit` alone doesn't handle duplicated submission gracefully. See https://github.com/ray-project/kuberay/issues/2154.
	// In order to deal with that, we use `ray job status` first to check if the jobId has been submitted.
	// If the jobId has been submitted, we use `ray job logs` to follow the logs.
	// Otherwise, we submit the job with `ray job submit --no-wait` + `ray job logs`. The full shell command looks like this:
	//   if ! ray job status --address http://$RAY_ADDRESS $RAY_JOB_SUBMISSION_ID >/dev/null 2>&1 ;
	//   then ray job submit --address http://$RAY_ADDRESS --submission-id $RAY_JOB_SUBMISSION_ID --no-wait -- ... ;
	//   fi ; ray job logs --address http://$RAY_ADDRESS --follow $RAY_JOB_SUBMISSION_ID
	// In Sidecar mode, the sidecar container's restart policy is set to Never, so duplicated submission won't happen.
	jobStatusCommand := []string{"ray", "job", "status", "--address", address, jobId, ">/dev/null", "2>&1"}
	jobSubmitCommand := []string{"ray", "job", "submit", "--address", address}
	jobFollowCommand := []string{"ray", "job", "logs", "--address", address, "--follow", jobId}

	if submissionMode == rayv1.SidecarMode {
		// Wait until Ray Dashboard GCS is healthy before proceeding.
		// Use the same Ray Dashboard GCS health check command as the readiness probe
		rayDashboardGCSHealthCommand := fmt.Sprintf(
			utils.BaseWgetHealthCommand,
			utils.DefaultReadinessProbeFailureThreshold,
			port,
			utils.RayDashboardGCSHealthPath,
		)

		waitLoop := []string{
			"until", rayDashboardGCSHealthCommand, ">/dev/null", "2>&1", ";",
			"do", "echo", strconv.Quote("Waiting for Ray Dashboard GCS to become healthy at " + address + " ..."), ";", "sleep", "2", ";", "done", ";",
		}
		cmd = append(cmd, waitLoop...)
	}

	// In Sidecar mode, we only support RayJob level retry, which means that the submitter retry won't happen,
	// so we won't have to check if the job has been submitted.
	if submissionMode == rayv1.K8sJobMode {
		// Only check job status in K8s mode to handle duplicated submission gracefully
		cmd = append(cmd, "if", "!")
		cmd = append(cmd, jobStatusCommand...)
		cmd = append(cmd, ";", "then")
	}

	cmd = append(cmd, jobSubmitCommand...)

	if submissionMode == rayv1.K8sJobMode {
		cmd = append(cmd, "--no-wait")
	}

	runtimeEnvJson, err := getRuntimeEnvJson(rayJobInstance)
	if err != nil {
		return nil, err
	}
	if len(runtimeEnvJson) > 0 {
		cmd = append(cmd, "--runtime-env-json", strconv.Quote(runtimeEnvJson))
	}

	if len(metadata) > 0 {
		metadataJson, err := GetMetadataJson(metadata, rayJobInstance.Spec.RayClusterSpec.RayVersion)
		if err != nil {
			return nil, err
		}
		cmd = append(cmd, "--metadata-json", strconv.Quote(metadataJson))
	}

	if len(jobId) > 0 {
		cmd = append(cmd, "--submission-id", jobId)
	}

	if entrypointNumCpus > 0 {
		cmd = append(cmd, "--entrypoint-num-cpus", fmt.Sprintf("%f", entrypointNumCpus))
	}

	if entrypointNumGpus > 0 {
		cmd = append(cmd, "--entrypoint-num-gpus", fmt.Sprintf("%f", entrypointNumGpus))
	}

	if len(entrypointResources) > 0 {
		cmd = append(cmd, "--entrypoint-resources", strconv.Quote(entrypointResources))
	}

	// "--" is used to separate the entrypoint from the Ray Job CLI command and its arguments.
	cmd = append(cmd, "--", entrypoint, ";")
	if submissionMode == rayv1.K8sJobMode {
		cmd = append(cmd, "fi", ";")
		cmd = append(cmd, jobFollowCommand...)
	}

	return cmd, nil
}

// GetDefaultSubmitterTemplate creates a default submitter template for the Ray job.
func GetDefaultSubmitterTemplate(rayClusterSpec *rayv1.RayClusterSpec) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				GetDefaultSubmitterContainer(rayClusterSpec),
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

// GetDefaultSubmitterContainer creates a default submitter container for the Ray job.
func GetDefaultSubmitterContainer(rayClusterSpec *rayv1.RayClusterSpec) corev1.Container {
	return corev1.Container{
		Name: utils.SubmitterContainerName,
		// Use the image of the Ray head to be defensive against version mismatch issues
		Image: rayClusterSpec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Image,
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("200Mi"),
			},
		},
	}
}
