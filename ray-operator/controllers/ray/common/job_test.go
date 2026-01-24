package common

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

func rayJobTemplate() *rayv1.RayJob {
	return &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			RuntimeEnvYAML: "test: test",
			Metadata: map[string]string{
				"testKey": "testValue",
			},
			RayClusterSpec: &rayv1.RayClusterSpec{
				RayVersion: "2.6.0",
			},
			Entrypoint:          "echo no quote 'single quote' \"double quote\"",
			EntrypointNumCpus:   1,
			EntrypointNumGpus:   0.5,
			EntrypointResources: `{"Custom_1": 1, "Custom_2": 5.5}`,
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://127.0.0.1:8265",
			JobId:        "testJobId",
		},
	}
}

func TestGetRuntimeEnvJsonFromBase64(t *testing.T) {
	testRayJob := rayJobTemplate()
	expected := `{"test":"test"}`
	jsonOutput, err := getRuntimeEnvJson(testRayJob)
	require.NoError(t, err)
	assert.JSONEq(t, expected, jsonOutput)
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
	require.NoError(t, err)

	var expectedMap map[string]any
	var actualMap map[string]any

	// Convert the JSON strings into map types to avoid errors due to ordering
	require.NoError(t, json.Unmarshal([]byte(expectedJSON), &expectedMap))
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &actualMap))

	// Now compare the maps
	assert.Equal(t, expectedMap, actualMap)
}

func TestGetMetadataJson(t *testing.T) {
	testRayJob := rayJobTemplate()
	expected := `{"testKey":"testValue"}`
	metadataJson, err := GetMetadataJson(testRayJob.Spec.Metadata, testRayJob.Spec.RayClusterSpec.RayVersion)
	require.NoError(t, err)
	assert.JSONEq(t, expected, metadataJson)
}

func TestBuildJobSubmitCommandWithK8sJobMode(t *testing.T) {
	testRayJob := rayJobTemplate()
	expected := []string{
		"if",
		"!", "ray", "job", "status", "--address", "http://127.0.0.1:8265", "testJobId", ">/dev/null", "2>&1",
		";", "then",
		"ray", "job", "submit", "--address", "http://127.0.0.1:8265", "--no-wait",
		"--runtime-env-json", strconv.Quote(`{"test":"test"}`),
		"--metadata-json", strconv.Quote(`{"testKey":"testValue"}`),
		"--submission-id", "testJobId",
		"--entrypoint-num-cpus", "1.000000",
		"--entrypoint-num-gpus", "0.500000",
		"--entrypoint-resources", strconv.Quote(`{"Custom_1": 1, "Custom_2": 5.5}`),
		"--",
		"echo no quote 'single quote' \"double quote\"",
		";", "fi", ";",
		"ray", "job", "logs", "--address", "http://127.0.0.1:8265", "--follow", "testJobId",
	}
	command, err := BuildJobSubmitCommand(testRayJob, rayv1.K8sJobMode)
	require.NoError(t, err)
	assert.Equal(t, expected, command)
}

func TestBuildJobSubmitCommandWithSidecarMode(t *testing.T) {
	testRayJob := rayJobTemplate()
	testRayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers = []corev1.Container{
		{
			Ports: []corev1.ContainerPort{
				{
					Name:          utils.DashboardPortName,
					ContainerPort: utils.DefaultDashboardPort,
				},
			},
		},
	}

	expected := []string{
		"until",
		fmt.Sprintf(
			utils.BaseWgetHealthCommand,
			utils.DefaultReadinessProbeFailureThreshold,
			utils.DefaultDashboardPort,
			utils.RayDashboardGCSHealthPath,
		),
		">/dev/null", "2>&1", ";",
		"do", "echo", strconv.Quote("Waiting for Ray Dashboard GCS to become healthy at http://127.0.0.1:8265 ..."), ";", "sleep", "2", ";", "done", ";",
		"ray", "job", "submit", "--address", "http://127.0.0.1:8265",
		"--runtime-env-json", strconv.Quote(`{"test":"test"}`),
		"--metadata-json", strconv.Quote(`{"testKey":"testValue"}`),
		"--submission-id", "testJobId",
		"--entrypoint-num-cpus", "1.000000",
		"--entrypoint-num-gpus", "0.500000",
		"--entrypoint-resources", strconv.Quote(`{"Custom_1": 1, "Custom_2": 5.5}`),
		"--",
		"echo no quote 'single quote' \"double quote\"",
		";",
	}
	command, err := BuildJobSubmitCommand(testRayJob, rayv1.SidecarMode)
	require.NoError(t, err)
	assert.Equal(t, expected, command)
}

func TestBuildJobSubmitCommandWithSidecarModeAndFeatureGate(t *testing.T) {
	// Enable the SidecarSubmitterRestart feature gate for this test
	features.SetFeatureGateDuringTest(t, features.SidecarSubmitterRestart, true)

	testRayJob := rayJobTemplate()
	testRayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers = []corev1.Container{
		{
			Ports: []corev1.ContainerPort{
				{
					Name:          utils.DashboardPortName,
					ContainerPort: utils.DefaultDashboardPort,
				},
			},
		},
	}

	// With SidecarSubmitterRestart enabled, the command should include:
	// - status check (if ! ray job status ...)
	// - --no-wait flag
	// - job logs follow at the end
	expected := []string{
		"until",
		fmt.Sprintf(
			utils.BaseWgetHealthCommand,
			utils.DefaultReadinessProbeFailureThreshold,
			utils.DefaultDashboardPort,
			utils.RayDashboardGCSHealthPath,
		),
		">/dev/null", "2>&1", ";",
		"do", "echo", strconv.Quote("Waiting for Ray Dashboard GCS to become healthy at http://127.0.0.1:8265 ..."), ";", "sleep", "2", ";", "done", ";",
		"if", "!", "ray", "job", "status", "--address", "http://127.0.0.1:8265", "testJobId", ">/dev/null", "2>&1", ";", "then",
		"ray", "job", "submit", "--address", "http://127.0.0.1:8265", "--no-wait",
		"--runtime-env-json", strconv.Quote(`{"test":"test"}`),
		"--metadata-json", strconv.Quote(`{"testKey":"testValue"}`),
		"--submission-id", "testJobId",
		"--entrypoint-num-cpus", "1.000000",
		"--entrypoint-num-gpus", "0.500000",
		"--entrypoint-resources", strconv.Quote(`{"Custom_1": 1, "Custom_2": 5.5}`),
		"--",
		"echo no quote 'single quote' \"double quote\"",
		";", "fi", ";",
		"ray", "job", "logs", "--address", "http://127.0.0.1:8265", "--follow", "testJobId",
	}
	command, err := BuildJobSubmitCommand(testRayJob, rayv1.SidecarMode)
	require.NoError(t, err)
	assert.Equal(t, expected, command)
}

func TestBuildJobSubmitCommandWithK8sJobModeAndYAML(t *testing.T) {
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
			Entrypoint: "echo no quote 'single quote' \"double quote\"",
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://127.0.0.1:8265",
			JobId:        "testJobId",
		},
	}
	expected := []string{
		"if",
		"!", "ray", "job", "status", "--address", "http://127.0.0.1:8265", "testJobId", ">/dev/null", "2>&1",
		";", "then",
		"ray", "job", "submit", "--address", "http://127.0.0.1:8265", "--no-wait",
		"--runtime-env-json", strconv.Quote(`{"working_dir":"https://github.com/ray-project/serve_config_examples/archive/b393e77bbd6aba0881e3d94c05f968f05a387b96.zip","pip":["python-multipart==0.0.6"]}`),
		"--metadata-json", strconv.Quote(`{"testKey":"testValue"}`),
		"--submission-id", "testJobId",
		"--",
		"echo no quote 'single quote' \"double quote\"",
		";", "fi", ";",
		"ray", "job", "logs", "--address", "http://127.0.0.1:8265", "--follow", "testJobId",
	}
	command, err := BuildJobSubmitCommand(rayJobWithYAML, rayv1.K8sJobMode)
	require.NoError(t, err)

	// Ensure the slices are the same length.
	assert.Len(t, command, len(expected))

	for i := 0; i < len(expected); i++ {
		// For non-JSON elements, compare them directly.
		assert.Equal(t, expected[i], command[i])
		if expected[i] == "--runtime-env-json" {
			// Decode the JSON string from the next element.
			var expectedMap, actualMap map[string]any
			unquoteExpected, err1 := strconv.Unquote(expected[i+1])
			require.NoError(t, err1)
			unquotedCommand, err2 := strconv.Unquote(command[i+1])
			require.NoError(t, err2)
			err1 = json.Unmarshal([]byte(unquoteExpected), &expectedMap)
			err2 = json.Unmarshal([]byte(unquotedCommand), &actualMap)

			// If there's an error decoding either JSON string, it's an error in the test.
			require.NoError(t, err1)
			require.NoError(t, err2)

			// Compare the maps directly to avoid errors due to ordering.
			assert.Equal(t, expectedMap, actualMap)

			// Skip the next element because we've just checked it.
			i++
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
	require.Error(t, err)
}

func TestGetSubmitterTemplate(t *testing.T) {
	rayJob := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{},
	}
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
	template := GetSubmitterTemplate(&rayJob.Spec, &rayCluster.Spec)
	assert.Equal(t, template.Spec.Containers[0].Image, rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Image)
}
