package util

import (
	api "github.com/ray-project/kuberay/proto/go_client"
	rayalphaapi "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RayJob struct {
	*rayalphaapi.RayJob
}

const rayJobDefaultVersion = "1.13"

// NewRayJob creates a RayJob.
func NewRayJob(apiJob *api.RayJob, computeTemplateMap map[string]*api.ComputeTemplate) *RayJob {
	var clusterSpec rayalphaapi.RayClusterSpec

	if apiJob.ClusterSpec != nil && len(apiJob.ClusterSelector) == 0 {
		clusterSpec = *buildRayClusterSpec(rayJobDefaultVersion, apiJob.ClusterSpec, computeTemplateMap)
	}

	rayJob := &rayalphaapi.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        apiJob.Name,
			Namespace:   apiJob.Namespace,
			Labels:      buildRayJobLabels(apiJob),
			Annotations: buildRayJobAnnotations(apiJob),
		},
		Spec: rayalphaapi.RayJobSpec{
			Entrypoint:               apiJob.Entrypoint,
			Metadata:                 apiJob.Metadata,
			RuntimeEnv:               apiJob.RuntimeEnv,
			ShutdownAfterJobFinishes: apiJob.ShutdownAfterJobFinishes,
			TTLSecondsAfterFinished:  &apiJob.TtlSecondsAfterFinished,
			JobId:                    apiJob.JobId,
			RayClusterSpec:           clusterSpec,
			ClusterSelector:          apiJob.ClusterSelector,
		},
	}

	return &RayJob{
		rayJob,
	}
}

func (j *RayJob) Get() *rayalphaapi.RayJob {
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

func buildRayJobAnnotations(cluster *api.RayJob) map[string]string {
	annotations := map[string]string{}
	return annotations
}
