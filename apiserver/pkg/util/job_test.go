package util

import (
	"testing"

	api "github.com/ray-project/kuberay/proto/go_client"

	"github.com/stretchr/testify/require"
)

var apiJobNewCluster = &api.RayJob{
	Name:       "test",
	Namespace:  "test",
	User:       "test",
	Entrypoint: "python /home/ray/samples/sample_code.py",
	Metadata: map[string]string{
		"job_submission_id": "123",
	},
	RuntimeEnv:               "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: \"test_counter\"\n",
	ShutdownAfterJobFinishes: true,
	ClusterSpec:              rayCluster.ClusterSpec,
}

var apiJobExistingCluster = &api.RayJob{
	Name:       "test",
	Namespace:  "test",
	User:       "test",
	Entrypoint: "python /home/ray/samples/sample_code.py",
	Metadata: map[string]string{
		"job_submission_id": "123",
	},
	RuntimeEnv: "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: \"test_counter\"\n",
	ClusterSelector: map[string]string{
		RayClusterUserLabelKey: "test",
	},
}

var apiJobExistingClusterSubmitter = &api.RayJob{
	Name:       "test",
	Namespace:  "test",
	User:       "test",
	Entrypoint: "python /home/ray/samples/sample_code.py",
	Metadata: map[string]string{
		"job_submission_id": "123",
	},
	RuntimeEnv: "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: \"test_counter\"\n",
	ClusterSelector: map[string]string{
		RayClusterUserLabelKey: "test",
	},
	JobSubmitter: &api.RayJobSubmitter{
		Image:  "image",
		Cpu:    "400m",
		Memory: "150Mi",
	},
	EntrypointNumCpus: 2,
}

var apiJobExistingClusterSubmitterBadParams = &api.RayJob{
	Name:       "test",
	Namespace:  "test",
	User:       "test",
	Entrypoint: "python /home/ray/samples/sample_code.py",
	Metadata: map[string]string{
		"job_submission_id": "123",
	},
	RuntimeEnv: "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: \"test_counter\"\n",
	ClusterSelector: map[string]string{
		RayClusterUserLabelKey: "test",
	},
	JobSubmitter: &api.RayJobSubmitter{
		Image:  "image",
		Memory: "1GB",
	},
	EntrypointNumCpus: 2,
}

func TestBuildRayJob(t *testing.T) {
	// Test request with cluster creation
	job, err := NewRayJob(apiJobNewCluster, map[string]*api.ComputeTemplate{"foo": &template})
	require.NoError(t, err)
	require.Equal(t, "test", job.ObjectMeta.Name)
	require.Equal(t, "test", job.ObjectMeta.Namespace)
	require.Len(t, job.ObjectMeta.Labels, 4)
	require.Equal(t, "test", job.ObjectMeta.Labels[RayClusterUserLabelKey])
	require.Len(t, job.ObjectMeta.Annotations, 1)
	require.Greater(t, len(job.Spec.RuntimeEnvYAML), 1)
	require.Len(t, job.Spec.Metadata, 1)
	require.Nil(t, job.Spec.ClusterSelector)
	require.NotNil(t, job.Spec.RayClusterSpec)

	// Test request without cluster creation
	job, err = NewRayJob(apiJobExistingCluster, map[string]*api.ComputeTemplate{"foo": &template})
	require.NoError(t, err)
	require.Equal(t, "test", job.ObjectMeta.Name)
	require.Equal(t, "test", job.ObjectMeta.Namespace)
	require.Len(t, job.ObjectMeta.Labels, 4)
	require.Equal(t, "test", job.ObjectMeta.Labels[RayClusterUserLabelKey])
	require.Greater(t, len(job.Spec.RuntimeEnvYAML), 1)
	require.NotNil(t, job.Spec.ClusterSelector)
	require.Nil(t, job.Spec.RayClusterSpec)
	require.Nil(t, job.Spec.SubmitterPodTemplate)

	// Test request without cluster creation with submitter
	job, err = NewRayJob(apiJobExistingClusterSubmitter, map[string]*api.ComputeTemplate{"foo": &template})
	require.NoError(t, err)
	require.Equal(t, "test", job.ObjectMeta.Name)
	require.Equal(t, "test", job.ObjectMeta.Namespace)
	require.Len(t, job.ObjectMeta.Labels, 4)
	require.Equal(t, "test", job.ObjectMeta.Labels[RayClusterUserLabelKey])
	require.Greater(t, len(job.Spec.RuntimeEnvYAML), 1)
	require.NotNil(t, job.Spec.ClusterSelector)
	require.Nil(t, job.Spec.RayClusterSpec)
	require.InEpsilon(t, float32(2), job.Spec.EntrypointNumCpus, 1e-9 /*epsilon*/)
	require.NotNil(t, job.Spec.SubmitterPodTemplate)
	require.Equal(t, "ray-job-submitter", job.Spec.SubmitterPodTemplate.Spec.Containers[0].Name)
	require.Equal(t, "image", job.Spec.SubmitterPodTemplate.Spec.Containers[0].Image)
	require.Equal(t, "400m", job.Spec.SubmitterPodTemplate.Spec.Containers[0].Resources.Limits.Cpu().String())
	require.Equal(t, "150Mi", job.Spec.SubmitterPodTemplate.Spec.Containers[0].Resources.Limits.Memory().String())
	require.Equal(t, "400m", job.Spec.SubmitterPodTemplate.Spec.Containers[0].Resources.Requests.Cpu().String())
	require.Equal(t, "150Mi", job.Spec.SubmitterPodTemplate.Spec.Containers[0].Resources.Requests.Memory().String())

	// Test request without cluster creation with submitter bad parameters
	_, err = NewRayJob(apiJobExistingClusterSubmitterBadParams, map[string]*api.ComputeTemplate{"foo": &template})
	require.Error(t, err)
}
