package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var testRayJob = &rayv1.RayJob{
	Spec: rayv1.RayJobSpec{
		RuntimeEnvYAML: "test: test",
		Metadata: map[string]string{
			"testKey": "testValue",
		},
		RayClusterSpec: &rayv1.RayClusterSpec{
			RayVersion: "2.6.0",
		},
		Entrypoint:          "echo hello",
		EntrypointNumCpus:   1,
		EntrypointNumGpus:   0.5,
		EntrypointResources: `{"Custom_1": 1, "Custom_2": 5.5}`,
	},
	Status: rayv1.RayJobStatus{
		DashboardURL: "http://127.0.0.1:8265",
		JobId:        "testJobId",
	},
}

func TestGetRuntimeEnvJsonFromBase64(t *testing.T) {
	expected := `{"test":"test"}`
	jsonOutput, err := getRuntimeEnvJson(testRayJob)
	assert.NoError(t, err)
	assert.Equal(t, expected, jsonOutput)
}

func TestGetRuntimeEnvJsonFromYAML(t *testing.T) {
	rayJobWithYAML := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			RuntimeEnvYAML: `
working_dir: "https://github.com/ray-project/serve_config_examples/archive/b393e77bbd6aba0881e3d94c05f968f05a387b96.zip"
pip: ["python-multipart==0.0.6"]
`,
		},
	}
	expectedJSON := `{"working_dir":"https://github.com/ray-project/serve_config_examples/archive/b393e77bbd6aba0881e3d94c05f968f05a387b96.zip","pip":["python-multipart==0.0.6"]}`
	jsonOutput, err := getRuntimeEnvJson(rayJobWithYAML)
	assert.NoError(t, err)

	var expectedMap map[string]interface{}
	var actualMap map[string]interface{}

	// Convert the JSON strings into map types to avoid errors due to ordering
	assert.NoError(t, json.Unmarshal([]byte(expectedJSON), &expectedMap))
	assert.NoError(t, json.Unmarshal([]byte(jsonOutput), &actualMap))

	// Now compare the maps
	assert.Equal(t, expectedMap, actualMap)
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
		"--entrypoint-num-cpus", "1.000000",
		"--entrypoint-num-gpus", "0.500000",
		"--entrypoint-resources", `{"Custom_1": 1, "Custom_2": 5.5}`,
		"--",
		"echo", "hello",
	}
	command, err := GetK8sJobCommand(testRayJob)
	assert.NoError(t, err)
	assert.Equal(t, expected, command)
}

func TestGetK8sJobCommandWithYAML(t *testing.T) {
	rayJobWithYAML := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			RuntimeEnvYAML: `
working_dir: "https://github.com/ray-project/serve_config_examples/archive/b393e77bbd6aba0881e3d94c05f968f05a387b96.zip"
pip: ["python-multipart==0.0.6"]
`,
			Metadata: map[string]string{
				"testKey": "testValue",
			},
			RayClusterSpec: &rayv1.RayClusterSpec{
				RayVersion: "2.6.0",
			},
			Entrypoint: "echo hello",
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://127.0.0.1:8265",
			JobId:        "testJobId",
		},
	}
	expected := []string{
		"ray", "job", "submit", "--address", "http://127.0.0.1:8265",
		"--runtime-env-json", `{"working_dir":"https://github.com/ray-project/serve_config_examples/archive/b393e77bbd6aba0881e3d94c05f968f05a387b96.zip","pip":["python-multipart==0.0.6"]}`,
		"--metadata-json", `{"testKey":"testValue"}`,
		"--submission-id", "testJobId",
		"--",
		"echo", "hello",
	}
	command, err := GetK8sJobCommand(rayJobWithYAML)
	assert.NoError(t, err)

	// Ensure the slices are the same length.
	assert.Equal(t, len(expected), len(command))

	for i := 0; i < len(expected); i++ {
		if expected[i] == "--runtime-env-json" {
			// Decode the JSON string from the next element.
			var expectedMap, actualMap map[string]interface{}
			err1 := json.Unmarshal([]byte(expected[i+1]), &expectedMap)
			err2 := json.Unmarshal([]byte(command[i+1]), &actualMap)

			// If there's an error decoding either JSON string, it's an error in the test.
			assert.NoError(t, err1)
			assert.NoError(t, err2)

			// Compare the maps directly to avoid errors due to ordering.
			assert.Equal(t, expectedMap, actualMap)

			// Skip the next element because we've just checked it.
			i++
		} else {
			// For non-JSON elements, compare them directly.
			assert.Equal(t, expected[i], command[i])
		}
	}
}

func TestMetadataRaisesErrorBeforeRay26(t *testing.T) {
	rayJob := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			RayClusterSpec: &rayv1.RayClusterSpec{
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

func TestGetDefaultSubmitterTemplate(t *testing.T) {
	rayCluster := &rayv1.RayCluster{
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Image: "rayproject/ray:test-submitter-template",
							},
						},
					},
				},
			},
		},
	}
	template := GetDefaultSubmitterTemplate(rayCluster)
	assert.Equal(t, template.Spec.Containers[0].Image, rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Image)
}
