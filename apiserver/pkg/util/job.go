package util

import (
	"errors"

	api "github.com/ray-project/kuberay/proto/go_client"
	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RayJob struct {
	*rayv1api.RayJob
}

const rayJobDefaultVersion = "1.13"

// NewRayJob creates a RayJob.
func NewRayJob(apiJob *api.RayJob, computeTemplateMap map[string]*api.ComputeTemplate) (*RayJob, error) {
	if apiJob.ClusterSelector != nil && apiJob.JobSubmitter == nil {
		// If cluster selector is specified, ensure that Job submitter is present
		return nil, errors.New("external Ray cluster requires Job submitter definition")
	}
	rayJob := &rayv1api.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        apiJob.Name,
			Namespace:   apiJob.Namespace,
			Labels:      buildRayJobLabels(apiJob),
			Annotations: buildRayJobAnnotations(apiJob),
		},
		Spec: rayv1api.RayJobSpec{
			Entrypoint:               apiJob.Entrypoint,
			Metadata:                 apiJob.Metadata,
			RuntimeEnvYAML:           apiJob.RuntimeEnv,
			ShutdownAfterJobFinishes: apiJob.ShutdownAfterJobFinishes,
			TTLSecondsAfterFinished:  &apiJob.TtlSecondsAfterFinished,
			JobId:                    apiJob.JobId,
			RayClusterSpec:           nil,
			ClusterSelector:          apiJob.ClusterSelector,
		},
	}
	if apiJob.ClusterSpec != nil {
		clusterSpec, err := buildRayClusterSpec(rayJobDefaultVersion, nil, apiJob.ClusterSpec, computeTemplateMap)
		if err != nil {
			return nil, err
		}
		rayJob.Spec.RayClusterSpec = clusterSpec
	}
	if apiJob.JobSubmitter != nil {
		// Job submitter is specified, create SubmitterPodTemplate
		cpus := "1"
		memorys := "1Gi"
		if apiJob.JobSubmitter.Cpu != "" {
			cpus = apiJob.JobSubmitter.Cpu
		}
		if apiJob.JobSubmitter.Memory != "" {
			memorys = apiJob.JobSubmitter.Memory
		}
		rayJob.Spec.SubmitterPodTemplate = &v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "ray-job-submitter",
						Image: apiJob.JobSubmitter.Image,
						Resources: v1.ResourceRequirements{
							Limits: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse(cpus),
								v1.ResourceMemory: resource.MustParse(memorys),
							},
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("500m"),
								v1.ResourceMemory: resource.MustParse("200Mi"),
							},
						},
					},
				},
				RestartPolicy: v1.RestartPolicyNever,
			},
		}
	}
	if apiJob.EntrypointNumCpus > 0 {
		// Entry point number of CPUs
		rayJob.Spec.EntrypointNumCpus = apiJob.EntrypointNumCpus
	}
	if apiJob.EntrypointNumGpus > 0 {
		// Entry point number of GPUs
		rayJob.Spec.DeepCopy().EntrypointNumGpus = apiJob.EntrypointNumGpus
	}
	if apiJob.EntrypointResources != "" {
		// Entry point resources
		rayJob.Spec.EntrypointResources = apiJob.EntrypointResources
	}

	return &RayJob{rayJob}, nil
}

func (j *RayJob) Get() *rayv1api.RayJob {
	return j.RayJob
}

func buildRayJobLabels(job *api.RayJob) map[string]string {
	labels := map[string]string{}
	labels[RayClusterNameLabelKey] = job.Name
	labels[RayClusterUserLabelKey] = job.User
	labels[KubernetesApplicationNameLabelKey] = ApplicationName
	labels[KubernetesManagedByLabelKey] = ComponentName
	return labels
}

func buildRayJobAnnotations(job *api.RayJob) map[string]string {
	return job.Metadata
}
