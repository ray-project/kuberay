package common

import (
	"testing"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/stretchr/testify/assert"
)

var testRayJob = &rayv1alpha1.RayJob{
	Spec: rayv1alpha1.RayJobSpec{
		RuntimeEnv: "eyJ0ZXN0IjoidGVzdCJ9", // {"test":"test"} in base64
		Metadata: map[string]string{
			"testKey": "testValue",
		},
		RayClusterSpec: &rayv1alpha1.RayClusterSpec{
			RayVersion: "2.6.0",
		},
		Entrypoint: "echo hello",
	},
	Status: rayv1alpha1.RayJobStatus{
		DashboardURL: "http://127.0.0.1:8265",
		JobId:        "testJobId",
	},
}

func TestGetDecodedRuntimeEnv(t *testing.T) {
	decoded, err := GetDecodedRuntimeEnv(testRayJob.Spec.RuntimeEnv)
	assert.NoError(t, err)
	assert.Equal(t, `{"test":"test"}`, decoded)
}

func TestGetBaseRayJobCommand(t *testing.T) {
	expected := []string{"ray", "job", "submit", "--address", "http://127.0.0.1:8265"}
	command := GetBaseRayJobCommand(testRayJob.Status.DashboardURL)
	assert.Equal(t, expected, command)
}

func TestGetMetadataJson(t *testing.T) {
	expected := `{"testKey":"testValue"}`
	metadataJson, err := GetMetadataJson(testRayJob.Spec.Metadata, testRayJob.Spec.RayClusterSpec.RayVersion)
	assert.NoError(t, err)
	assert.Equal(t, expected, metadataJson)
}

func TestGetK8sJobCommand(t *testing.T) {
	expected := []string{
		"ray", "job", "submit", "--address", "http://127.0.0.1:8265",
		"--runtime-env-json", `{"test":"test"}`,
		"--metadata-json", `{"testKey":"testValue"}`,
		"--submission-id", "testJobId",
		"--",
		"echo", "hello",
	}
	command, err := GetK8sJobCommand(testRayJob)
	assert.NoError(t, err)
	assert.Equal(t, expected, command)
}

func TestMetadataRaisesErrorBeforeRay26(t *testing.T) {
	rayJob := &rayv1alpha1.RayJob{
		Spec: rayv1alpha1.RayJobSpec{
			RayClusterSpec: &rayv1alpha1.RayClusterSpec{
				RayVersion: "2.5.0",
			},
			Metadata: map[string]string{
				"testKey": "testValue",
			},
		},
	}
	_, err := GetMetadataJson(rayJob.Spec.Metadata, rayJob.Spec.RayClusterSpec.RayVersion)
	assert.Error(t, err)
}