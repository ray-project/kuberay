package util

import (
	"testing"

	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/stretchr/testify/assert"
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

func TestBuildRayJob(t *testing.T) {
	// Test request with cluster creation
	job, err := NewRayJob(apiJobNewCluster, map[string]*api.ComputeTemplate{"foo": &template})
	assert.Nil(t, err)
	assert.Equal(t, "test", job.ObjectMeta.Name)
	assert.Equal(t, "test", job.ObjectMeta.Namespace)
	assert.Equal(t, 4, len(job.ObjectMeta.Labels))
	assert.Equal(t, "test", job.ObjectMeta.Labels[RayClusterUserLabelKey])
	assert.Equal(t, 1, len(job.ObjectMeta.Annotations))
	assert.Greater(t, len(job.Spec.RuntimeEnvYAML), 1)
	assert.Equal(t, len(job.Spec.RuntimeEnv), 0)
	assert.Equal(t, 1, len(job.Spec.Metadata))
	assert.Nil(t, job.Spec.ClusterSelector)
	assert.NotNil(t, job.Spec.RayClusterSpec)

	// Test request with cluster creation
	job, err = NewRayJob(apiJobExistingCluster, map[string]*api.ComputeTemplate{"foo": &template})
	assert.Nil(t, err)
	assert.Equal(t, "test", job.ObjectMeta.Name)
	assert.Equal(t, "test", job.ObjectMeta.Namespace)
	assert.Equal(t, 4, len(job.ObjectMeta.Labels))
	assert.Equal(t, "test", job.ObjectMeta.Labels[RayClusterUserLabelKey])
	assert.Greater(t, len(job.Spec.RuntimeEnvYAML), 1)
	assert.Equal(t, len(job.Spec.RuntimeEnv), 0)
	assert.NotNil(t, job.Spec.ClusterSelector)
	assert.Nil(t, job.Spec.RayClusterSpec)
}
