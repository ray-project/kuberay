package common

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

const testDashboardAddressResolution = `dashboard_address='http://127.0.0.1:8265';
dashboard_address="${RAY_DASHBOARD_ADDRESS:-$dashboard_address}";
case "$dashboard_address" in
  http://*|https://*) ;;
  *) dashboard_address="http://$dashboard_address" ;;
esac;
while [ "${dashboard_address%/}" != "$dashboard_address" ]; do
  dashboard_address="${dashboard_address%/}";
done;`

func TestBuildJobSubmitCommandWithK8sJobMode(t *testing.T) {
	testRayJob := rayJobTemplate()
	expected := []string{
		testDashboardAddressResolution,
		"until",
		`python -c "import sys, urllib.request; r=urllib.request.urlopen(sys.argv[1], timeout=10); exit(0 if b'success' in r.read() else 1)" "$dashboard_address/api/gcs_healthz"`,
		">/dev/null", "2>&1", ";",
		"do", "echo", `"Waiting for Ray Dashboard GCS to become healthy at $dashboard_address ..."`, ";", "sleep", "2", ";", "done", ";",
		"if",
		"!", "ray", "job", "status", "--address", `"$dashboard_address"`, "testJobId", ">/dev/null", "2>&1",
		";", "then",
		"ray", "job", "submit", "--address", `"$dashboard_address"`, "--no-wait",
		"--runtime-env-json", strconv.Quote(`{"test":"test"}`),
		"--metadata-json", strconv.Quote(`{"testKey":"testValue"}`),
		"--submission-id", "testJobId",
		"--entrypoint-num-cpus", "1.000000",
		"--entrypoint-num-gpus", "0.500000",
		"--entrypoint-resources", strconv.Quote(`{"Custom_1": 1, "Custom_2": 5.5}`),
		"--",
		"echo no quote 'single quote' \"double quote\"",
		";", "fi", ";",
		"ray", "job", "logs", "--address", `"$dashboard_address"`, "--follow", "testJobId",
	}
	command, err := BuildJobSubmitCommand(testRayJob, rayv1.K8sJobMode)
	require.NoError(t, err)
	assert.Equal(t, expected, command)
}

func TestBuildJobSubmitCommandDashboardAddressRuntime(t *testing.T) {
	tests := []struct {
		name     string
		override string
		fallback string
		expected string
		unset    bool
	}{
		{name: "unset", unset: true, expected: "http://head-svc:8265"},
		{name: "empty", expected: "http://head-svc:8265"},
		{name: "host and port", override: "proxy:8265", expected: "http://proxy:8265"},
		{name: "http", override: "http://proxy:8265", expected: "http://proxy:8265"},
		{name: "https", override: "https://proxy:443", expected: "https://proxy:443"},
		{name: "host trailing slash", override: "proxy:8265/", expected: "http://proxy:8265"},
		{name: "http trailing slashes", override: "http://proxy:8265///", expected: "http://proxy:8265"},
		{name: "https base path", override: "https://proxy:443/ray///", expected: "https://proxy:443/ray"},
		{name: "https fallback", fallback: "https://head-svc:8265///", expected: "https://head-svc:8265"},
		{name: "literal override", override: "https://proxy:443/a'\"$HOME`exit 1`$(exit 1) space///", expected: "https://proxy:443/a'\"$HOME`exit 1`$(exit 1) space"},
		{name: "literal fallback", fallback: "https://head-svc:8265/a'\"$HOME`exit 1`$(exit 1) space///", expected: "https://head-svc:8265/a'\"$HOME`exit 1`$(exit 1) space"},
	}
	for _, tt := range tests {
		for _, existingJob := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", tt.name, existingJob), func(t *testing.T) {
				rayJob := rayJobTemplate()
				rayJob.Status.DashboardURL = "head-svc:8265"
				if tt.fallback != "" {
					rayJob.Status.DashboardURL = tt.fallback
				}
				command, err := BuildJobSubmitCommand(rayJob, rayv1.K8sJobMode)
				require.NoError(t, err)

				// Record argument boundaries, and fail the first health check to exercise
				// the waiting message and retry interval without actually sleeping.
				dir := t.TempDir()
				stubs := map[string]string{
					"python": `printf '%s\0' health "$3" >> "$CALL_LOG"
printf '\n' >> "$CALL_LOG"
if [ ! -e "$HEALTH_READY" ]; then
  : > "$HEALTH_READY"
  exit 1
fi
`,
					"sleep": `printf '%s\0' sleep "$@" >> "$CALL_LOG"
printf '\n' >> "$CALL_LOG"
`,
					"ray": `printf '%s\0' ray "$@" >> "$CALL_LOG"
printf '\n' >> "$CALL_LOG"
if [ "$2" = status ]; then exit "$STATUS_EXIT_CODE"; fi
`,
				}
				for name, script := range stubs {
					require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o700))
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "/bin/bash", "-ce", "--", strings.Join(command, " "))
				cmd.WaitDelay = time.Second
				// Use a controlled environment so the unset case cannot inherit an override.
				cmd.Env = []string{
					"PATH=" + dir,
					"CALL_LOG=" + filepath.Join(dir, "calls"),
					"HEALTH_READY=" + filepath.Join(dir, "healthy"),
					"STATUS_EXIT_CODE=1",
				}
				if existingJob {
					cmd.Env[len(cmd.Env)-1] = "STATUS_EXIT_CODE=0"
				}
				if !tt.unset {
					cmd.Env = append(cmd.Env, utils.RAY_DASHBOARD_ADDRESS+"="+tt.override)
				}
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				assert.Equal(t, "Waiting for Ray Dashboard GCS to become healthy at "+tt.expected+" ...\n", string(output))

				calls, err := os.ReadFile(filepath.Join(dir, "calls"))
				require.NoError(t, err)
				var actual [][]string
				for _, call := range strings.Split(strings.TrimSuffix(string(calls), "\n"), "\n") {
					actual = append(actual, strings.Split(strings.TrimSuffix(call, "\x00"), "\x00"))
				}
				expected := [][]string{
					{"health", tt.expected + "/api/gcs_healthz"},
					{"sleep", "2"},
					{"health", tt.expected + "/api/gcs_healthz"},
					{"ray", "job", "status", "--address", tt.expected, "testJobId"},
				}
				if !existingJob {
					expected = append(expected, []string{
						"ray", "job", "submit", "--address", tt.expected, "--no-wait",
						"--runtime-env-json", `{"test":"test"}`,
						"--metadata-json", `{"testKey":"testValue"}`,
						"--submission-id", "testJobId",
						"--entrypoint-num-cpus", "1.000000", "--entrypoint-num-gpus", "0.500000",
						"--entrypoint-resources", `{"Custom_1": 1, "Custom_2": 5.5}`,
						"--", "echo", "no", "quote", "single quote", "double quote",
					})
				}
				expected = append(expected, []string{"ray", "job", "logs", "--address", tt.expected, "--follow", "testJobId"})
				assert.Equal(t, expected, actual)
			})
		}
	}
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
		testDashboardAddressResolution,
		"until",
		`python -c "import sys, urllib.request; r=urllib.request.urlopen(sys.argv[1], timeout=10); exit(0 if b'success' in r.read() else 1)" "$dashboard_address/api/gcs_healthz"`,
		">/dev/null", "2>&1", ";",
		"do", "echo", `"Waiting for Ray Dashboard GCS to become healthy at $dashboard_address ..."`, ";", "sleep", "2", ";", "done", ";",
		"if",
		"!", "ray", "job", "status", "--address", `"$dashboard_address"`, "testJobId", ">/dev/null", "2>&1",
		";", "then",
		"ray", "job", "submit", "--address", `"$dashboard_address"`, "--no-wait",
		"--runtime-env-json", strconv.Quote(`{"working_dir":"https://github.com/ray-project/serve_config_examples/archive/b393e77bbd6aba0881e3d94c05f968f05a387b96.zip","pip":["python-multipart==0.0.6"]}`),
		"--metadata-json", strconv.Quote(`{"testKey":"testValue"}`),
		"--submission-id", "testJobId",
		"--",
		"echo no quote 'single quote' \"double quote\"",
		";", "fi", ";",
		"ray", "job", "logs", "--address", `"$dashboard_address"`, "--follow", "testJobId",
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
