package common

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
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

	address := "http://127.0.0.1:8265"
	expected := []string{
		// Wait for Dashboard GCS health
		"until",
		fmt.Sprintf(
			utils.BaseWgetHealthCommand,
			utils.DefaultReadinessProbeFailureThreshold,
			utils.DefaultDashboardPort,
			utils.RayDashboardGCSHealthPath,
		),
		">/dev/null", "2>&1", ";",
		"do", "echo", strconv.Quote("Waiting for Ray Dashboard GCS to become healthy at " + address + " ..."), ";", "sleep", "2", ";", "done", ";",
		// Wait for expected nodes to register
		"if", "[", "-n", "\"$" + utils.RAY_EXPECTED_MIN_WORKERS + "\"", "]", "&&", "[", "\"$" + utils.RAY_EXPECTED_MIN_WORKERS + "\"", "-gt", "\"0\"", "]", ";", "then",
		"EXPECTED_NODES=$(($" + utils.RAY_EXPECTED_MIN_WORKERS + " + 1))", ";",
		"echo", strconv.Quote("Waiting for $EXPECTED_NODES nodes (1 head + $" + utils.RAY_EXPECTED_MIN_WORKERS + " workers) to register..."), ";",
		"until", "[",
		"\"$(python3 -c \"import urllib.request,json,os; req=urllib.request.Request('" + address + "/nodes?view=summary'); t=os.environ.get('" + utils.RAY_AUTH_TOKEN_ENV_VAR + "',''); t and req.add_header('x-ray-authorization','Bearer '+t); d=json.loads(urllib.request.urlopen(req,timeout=5).read()); print(len([n for n in d.get('data',{}).get('summary',[]) if n.get('raylet',{}).get('state')=='ALIVE']))\" 2>/dev/null || echo 0)\"",
		"-ge", "\"$EXPECTED_NODES\"", "]", ";",
		"do", "echo", strconv.Quote("Waiting for Ray nodes to register. Expected: $EXPECTED_NODES ..."), ";", "sleep", "2", ";", "done", ";",
		"echo", strconv.Quote("All expected nodes are registered."), ";",
		"fi", ";",
		// Job submit command
		"ray", "job", "submit", "--address", address,
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

func TestGetMinReplicasFromSpec(t *testing.T) {
	tests := []struct {
		spec     *rayv1.RayClusterSpec
		name     string
		expected int32
	}{
		{
			name:     "nil spec returns 0",
			spec:     nil,
			expected: 0,
		},
		{
			name: "no worker groups returns 0",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{},
			},
			expected: 0,
		},
		{
			name: "single worker group with minReplicas",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						MinReplicas: ptr.To[int32](2),
						NumOfHosts:  1,
					},
				},
			},
			expected: 2,
		},
		{
			name: "multiple worker groups",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						MinReplicas: ptr.To[int32](2),
						NumOfHosts:  1,
					},
					{
						MinReplicas: ptr.To[int32](3),
						NumOfHosts:  1,
					},
				},
			},
			expected: 5,
		},
		{
			name: "worker group with NumOfHosts > 1",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						MinReplicas: ptr.To[int32](2),
						NumOfHosts:  2,
					},
				},
			},
			expected: 4,
		},
		{
			name: "suspended worker group is skipped",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						MinReplicas: ptr.To[int32](2),
						NumOfHosts:  1,
						Suspend:     ptr.To(true),
					},
					{
						MinReplicas: ptr.To[int32](3),
						NumOfHosts:  1,
					},
				},
			},
			expected: 3,
		},
		{
			name: "nil minReplicas defaults to 0",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						MinReplicas: nil,
						NumOfHosts:  1,
					},
				},
			},
			expected: 0,
		},
		{
			name: "NumOfHosts 0 results in 0 workers for that group",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						MinReplicas: ptr.To[int32](2),
						NumOfHosts:  0,
					},
				},
			},
			expected: 0,
		},
		{
			name: "falls back to Replicas when MinReplicas is nil",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						Replicas:    ptr.To[int32](3),
						MinReplicas: nil,
						NumOfHosts:  1,
					},
				},
			},
			expected: 3,
		},
		{
			name: "falls back to Replicas when MinReplicas is 0",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						Replicas:    ptr.To[int32](5),
						MinReplicas: ptr.To[int32](0),
						NumOfHosts:  1,
					},
				},
			},
			expected: 5,
		},
		{
			name: "uses MinReplicas when both are set and MinReplicas > 0",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						Replicas:    ptr.To[int32](5),
						MinReplicas: ptr.To[int32](2),
						NumOfHosts:  1,
					},
				},
			},
			expected: 2,
		},
		{
			name: "both MinReplicas and Replicas are nil",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						Replicas:    nil,
						MinReplicas: nil,
						NumOfHosts:  1,
					},
				},
			},
			expected: 0,
		},
		{
			name: "both MinReplicas and Replicas are 0",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						Replicas:    ptr.To[int32](0),
						MinReplicas: ptr.To[int32](0),
						NumOfHosts:  1,
					},
				},
			},
			expected: 0,
		},
		{
			name: "mixed worker groups - some with MinReplicas some with only Replicas",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						MinReplicas: ptr.To[int32](2),
						NumOfHosts:  1,
					},
					{
						Replicas:    ptr.To[int32](3),
						MinReplicas: nil,
						NumOfHosts:  1,
					},
					{
						Replicas:    ptr.To[int32](4),
						MinReplicas: ptr.To[int32](0),
						NumOfHosts:  1,
					},
				},
			},
			expected: 9, // 2 + 3 + 4
		},
		{
			name: "NumOfHosts > 1 with Replicas fallback",
			spec: &rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						Replicas:    ptr.To[int32](3),
						MinReplicas: nil,
						NumOfHosts:  2,
					},
				},
			},
			expected: 6, // 3 * 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetMinReplicasFromSpec(tt.spec)
			assert.Equal(t, tt.expected, result)
		})
	}
}
