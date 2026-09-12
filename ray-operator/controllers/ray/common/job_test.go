package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

const expectedK8sJobHealthCommand = `python -c '
import sys
import urllib.request
from ray.dashboard.utils import get_address_for_submission_client

address = get_address_for_submission_client(sys.argv[1])
health_url = address.rstrip("/") + "/api/gcs_healthz"
with urllib.request.urlopen(health_url, timeout=10) as response:
    sys.exit(0 if b"success" in response.read() else 1)
' 'http://127.0.0.1:8265'`

func TestBuildJobSubmitCommandWithK8sJobMode(t *testing.T) {
	testRayJob := rayJobTemplate()
	expected := []string{
		"until",
		expectedK8sJobHealthCommand,
		">/dev/null", "2>&1", ";",
		"do", "echo", strconv.Quote("Waiting for Ray Dashboard GCS to become healthy at http://127.0.0.1:8265 ..."), ";", "sleep", "2", ";", "done", ";",
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
			utils.BasePythonHealthCommand,
			fmt.Sprintf("http://localhost:%d/%s", utils.DefaultDashboardPort, utils.RayDashboardGCSHealthPath),
			utils.RayDashboardGCSHealthCheckTimeoutSeconds,
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

func TestBuildJobSubmitCommandWithSidecarModeVersionSwitch(t *testing.T) {
	tests := []struct {
		name       string
		rayVersion string
	}{
		{
			name:       "uses python health command for ray >= 2.53",
			rayVersion: "2.53.0",
		},
		{
			name:       "uses python health command for ray < 2.53",
			rayVersion: "2.52.1",
		},
		{
			name:       "uses python health command when rayVersion is invalid",
			rayVersion: "invalid-version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testRayJob := rayJobTemplate()
			testRayJob.Spec.RayClusterSpec.RayVersion = tt.rayVersion
			// Avoid metadata-json version parsing failure; this test only checks health command selection.
			testRayJob.Spec.Metadata = nil
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
			command, err := BuildJobSubmitCommand(testRayJob, rayv1.SidecarMode)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(command), 2)
			assert.Equal(t, "until", command[0])
			assert.Contains(t, command[1], "python -c")
			assert.Contains(t, command[1], utils.RayDashboardGCSHealthPath)
			assert.NotContains(t, command[1], "wget")
		})
	}
}

func TestBuildJobSubmitCommandWithSidecarModeCustomDashboardPort(t *testing.T) {
	testRayJob := rayJobTemplate()
	const customPort = 9000
	testRayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers = []corev1.Container{
		{
			Ports: []corev1.ContainerPort{
				{
					Name:          utils.DashboardPortName,
					ContainerPort: customPort,
				},
			},
		},
	}
	command, err := BuildJobSubmitCommand(testRayJob, rayv1.SidecarMode)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(command), 2)
	assert.Equal(t, "until", command[0])
	assert.Contains(t, command[1], fmt.Sprintf("localhost:%d/%s", customPort, utils.RayDashboardGCSHealthPath))
	assert.Contains(t, command[1], "python -c")
	assert.NotContains(t, command[1], "wget")
}

func TestBuildJobSubmitCommandWithK8sJobModeHealthWaitLoop(t *testing.T) {
	testRayJob := rayJobTemplate()
	command, err := BuildJobSubmitCommand(testRayJob, rayv1.K8sJobMode)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(command), 2)
	assert.Equal(t, "until", command[0])
	assert.Contains(t, command[1], "python -c")
	assert.Contains(t, command[1], utils.RayDashboardGCSHealthPath)
	assert.Contains(t, command[1], "127.0.0.1:8265")
	assert.NotContains(t, command[1], "wget")
}

func TestBuildJobSubmitCommandHealthProbeRuntime(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required to execute the generated health probe")
	}
	// Resolve wrappers such as pyenv before putting a python symlink on PATH.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pythonPath, err := exec.CommandContext(ctx, python, "-c", "import sys; print(sys.executable)").Output()
	require.NoError(t, err)

	tests := []struct {
		name            string
		body            string
		status          int
		fallback        bool
		literalArgument bool
		retryHTTP       bool
		retryResolution bool
		wantSuccess     bool
	}{
		{name: "resolved address replaces unresolvable fallback", status: http.StatusOK, body: "success", wantSuccess: true},
		{name: "generated address fallback", status: http.StatusOK, body: "success", fallback: true, wantSuccess: true},
		{name: "literal fallback argument", status: http.StatusOK, body: "success", literalArgument: true, wantSuccess: true},
		{name: "unhealthy body", status: http.StatusOK, body: "unhealthy"},
		{name: "HTTP error", status: http.StatusServiceUnavailable, body: "unhealthy"},
		{name: "retry HTTP error", status: http.StatusOK, body: "success", retryHTTP: true, wantSuccess: true},
		{name: "retry resolution error", status: http.StatusOK, body: "success", retryResolution: true, wantSuccess: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/dashboard/api/gcs_healthz", r.URL.Path)
				if requests.Add(1) == 1 && tt.retryHTTP {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(tt.status)
				_, err := w.Write([]byte(tt.body))
				assert.NoError(t, err)
			}))
			defer server.Close()

			rayJob := rayJobTemplate()
			rayJob.Status.DashboardURL = "http://unresolvable.invalid:8265"
			resolvedAddress := server.URL + "/dashboard///"
			if tt.fallback {
				rayJob.Status.DashboardURL = resolvedAddress
			}
			if tt.literalArgument {
				rayJob.Status.DashboardURL += "/a'\"$HOME`exit 1`$(exit 1) space"
			}
			command, err := BuildJobSubmitCommand(rayJob, rayv1.K8sJobMode)
			require.NoError(t, err)

			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "ray", "dashboard"), 0o700))
			for _, path := range []string{"ray/__init__.py", "ray/dashboard/__init__.py"} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, path), nil, 0o600))
			}
			// Stub only Ray's resolver, not Python or HTTP: execute the real generated
			// probe against a local server and assert it uses the resolver's output.
			resolver := `import os
from pathlib import Path

def get_address_for_submission_client(address):
    assert address == os.environ["EXPECTED_FALLBACK"], repr(address)
    calls = Path(os.environ["RESOLVER_CALLS"])
    first_call = not calls.exists()
    with calls.open("a") as output:
        output.write("resolved\n")
    if first_call and os.environ["RETRY_RESOLUTION"] == "true":
        raise RuntimeError("transient resolution failure")
    return os.environ["RESOLVED_ADDRESS"]
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "ray", "dashboard", "utils.py"), []byte(resolver), 0o600))
			require.NoError(t, os.Symlink(strings.TrimSpace(string(pythonPath)), filepath.Join(dir, "python")))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "sleep"), []byte("#!/bin/sh\n[ \"$1\" = 2 ]\n"), 0o700))

			script := command[1]
			if tt.retryHTTP || tt.retryResolution {
				statusCheck := slices.Index(command, "if")
				require.Positive(t, statusCheck)
				script = strings.Join(command[:statusCheck], " ")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/bash", "-ce", "--", script)
			cmd.WaitDelay = time.Second
			cmd.Env = []string{
				"PATH=" + dir,
				"PYTHONPATH=" + dir,
				"PYTHONNOUSERSITE=1",
				"EXPECTED_FALLBACK=" + rayJob.Status.DashboardURL,
				"RESOLVED_ADDRESS=" + resolvedAddress,
				"RESOLVER_CALLS=" + filepath.Join(dir, "calls"),
				"RETRY_RESOLUTION=" + strconv.FormatBool(tt.retryResolution),
			}
			output, err := cmd.CombinedOutput()
			if tt.wantSuccess {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err)
			}
			wantCalls := 1
			if tt.retryHTTP || tt.retryResolution {
				wantCalls = 2
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			require.NoError(t, err)
			assert.Equal(t, strings.Repeat("resolved\n", wantCalls), string(calls))
			wantRequests := int32(1)
			if tt.retryHTTP {
				wantRequests = 2
			}
			assert.Equal(t, wantRequests, requests.Load())
		})
	}
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
	healthURL := fmt.Sprintf("http://localhost:%d/%s", utils.DefaultDashboardPort, utils.RayDashboardGCSHealthPath)
	expected := []string{
		"until",
		fmt.Sprintf(
			utils.BasePythonHealthCommand,
			healthURL,
			utils.RayDashboardGCSHealthCheckTimeoutSeconds,
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
		"until",
		expectedK8sJobHealthCommand,
		">/dev/null", "2>&1", ";",
		"do", "echo", strconv.Quote("Waiting for Ray Dashboard GCS to become healthy at http://127.0.0.1:8265 ..."), ";", "sleep", "2", ";", "done", ";",
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
			//nolint:gosec // G602: test invariant guarantees "--runtime-env-json" is followed by a value.
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

// TestBuildJobSubmitCommandWithClusterSelector verifies that metadata is included
// when submitting a RayJob to an existing cluster via clusterSelector (no RayClusterSpec,
// so we can't know the Ray version without looking up the RayCluster; we assume >= 2.6).
func TestBuildJobSubmitCommandWithClusterSelector(t *testing.T) {
	rayJobWithClusterSelector := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			ClusterSelector: map[string]string{
				"ray.io/cluster": "existing-cluster",
			},
			Metadata: map[string]string{
				"tenant": "tenant1",
				"team":   "ml-platform",
			},
			Entrypoint: "python /app/batch_inference.py",
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://existing-cluster-head-svc:8265",
			JobId:        "cluster-selector-job-id",
		},
	}

	command, err := BuildJobSubmitCommand(rayJobWithClusterSelector, rayv1.K8sJobMode)
	require.NoError(t, err)

	hasMetadataFlag := false
	for i, arg := range command {
		if arg == "--metadata-json" {
			hasMetadataFlag = true
			require.Greater(t, len(command), i+1)
			unquoted, err := strconv.Unquote(command[i+1])
			require.NoError(t, err)
			var metadata map[string]string
			require.NoError(t, json.Unmarshal([]byte(unquoted), &metadata))
			assert.Equal(t, "tenant1", metadata["tenant"])
			assert.Equal(t, "ml-platform", metadata["team"])
			break
		}
	}
	assert.True(t, hasMetadataFlag, "metadata-json flag should be present when using clusterSelector with metadata")
}

// TestBuildJobSubmitCommandWithUnparseableRayVersion verifies that metadata is
// rejected when RayJob.Spec.RayClusterSpec.RayVersion is set to a non-semver string. A user
// who went to the trouble of typing a version should fail fast rather than silently proceed.
func TestBuildJobSubmitCommandWithUnparseableRayVersion(t *testing.T) {
	rayJob := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			RayClusterSpec: &rayv1.RayClusterSpec{
				RayVersion: "not-a-version",
			},
			Metadata: map[string]string{
				"testKey": "testValue",
			},
			Entrypoint: "echo hello",
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://127.0.0.1:8265",
			JobId:        "testJobId",
		},
	}
	_, err := BuildJobSubmitCommand(rayJob, rayv1.K8sJobMode)
	require.Error(t, err)
}

// TestBuildJobSubmitCommandWithOldRayVersion verifies that metadata is
// rejected when RayJob.Spec.RayClusterSpec.RayVersion is explicitly set below 2.6.0.
func TestBuildJobSubmitCommandWithOldRayVersion(t *testing.T) {
	rayJob := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			RayClusterSpec: &rayv1.RayClusterSpec{
				RayVersion: "2.5.0",
			},
			Metadata: map[string]string{
				"testKey": "testValue",
			},
			Entrypoint: "echo hello",
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://127.0.0.1:8265",
			JobId:        "testJobId",
		},
	}
	_, err := BuildJobSubmitCommand(rayJob, rayv1.K8sJobMode)
	require.Error(t, err)
}

// TestBuildJobSubmitCommandWithUnsetRayVersion verifies that
// metadata is still included when RayClusterSpec is present but RayVersion is empty. We assume
// the cluster is >= 2.6.0 unless the user explicitly sets a lower version.
func TestBuildJobSubmitCommandWithUnsetRayVersion(t *testing.T) {
	rayJob := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			RayClusterSpec: &rayv1.RayClusterSpec{},
			Metadata: map[string]string{
				"testKey": "testValue",
			},
			Entrypoint: "echo hello",
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://127.0.0.1:8265",
			JobId:        "testJobId",
		},
	}
	command, err := BuildJobSubmitCommand(rayJob, rayv1.K8sJobMode)
	require.NoError(t, err)

	hasMetadataFlag := false
	for i, arg := range command {
		if arg == "--metadata-json" {
			hasMetadataFlag = true
			require.Greater(t, len(command), i+1)
			unquoted, err := strconv.Unquote(command[i+1])
			require.NoError(t, err)
			var metadata map[string]string
			require.NoError(t, json.Unmarshal([]byte(unquoted), &metadata))
			assert.Equal(t, "testValue", metadata["testKey"])
			break
		}
	}
	assert.True(t, hasMetadataFlag, "metadata-json flag should be present when RayVersion is unset")
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
