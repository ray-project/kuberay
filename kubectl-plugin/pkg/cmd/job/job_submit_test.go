package job

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestRayJobSubmitComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))
	fakeSubmitJobOptions := NewJobSubmitOptions(cmdFactory, testStreams)
	fakeSubmitJobOptions.runtimeEnv = "././fake/path/to/env/yaml"
	fakeSubmitJobOptions.fileName = "fake/path/to/rayjob.yaml"

	cmd := &cobra.Command{}
	cmd.Flags().StringVarP(&fakeSubmitJobOptions.namespace, "namespace", "n", "", "")

	err := fakeSubmitJobOptions.Complete(cmd)
	require.NoError(t, err)
	assert.Equal(t, "default", fakeSubmitJobOptions.namespace)
	assert.Equal(t, "fake/path/to/env/yaml", fakeSubmitJobOptions.runtimeEnv)
}

func TestRayJobSubmitValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	fakeDir := t.TempDir()

	rayYaml := `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`

	rayJobYamlPath := filepath.Join(fakeDir, "rayjob-temp-*.yaml")

	file, err := os.Create(rayJobYamlPath)
	require.NoError(t, err)

	_, err = file.Write([]byte(rayYaml))
	require.NoError(t, err)

	tests := []struct {
		name        string
		opts        *SubmitJobOptions
		expectError string
	}{
		{
			name: "Successful submit job validation with RayJob",
			opts: &SubmitJobOptions{
				cmdFactory: cmdFactory,
				ioStreams:  &testStreams,
				fileName:   rayJobYamlPath,
				workingDir: "Fake/File/Path",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDecodeRayJobYaml(t *testing.T) {
	rayjobtmpfile, err := os.CreateTemp("./", "rayjob-temp-*.yaml")
	require.NoError(t, err)

	defer os.Remove(rayjobtmpfile.Name())

	rayYaml := `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`
	_, err = rayjobtmpfile.Write([]byte(rayYaml))
	require.NoError(t, err)

	rayJobYamlActual, err := decodeRayJobYaml(filepath.Join("./", rayjobtmpfile.Name()))
	require.NoError(t, err)

	assert.Equal(t, "rayjob-sample", rayJobYamlActual.GetName())

	submissionMode := rayJobYamlActual.Spec.SubmissionMode
	assert.Equal(t, rayv1.InteractiveMode, submissionMode)
}

func TestRuntimeEnvHasWorkingDir(t *testing.T) {
	runtimeEnvFile, err := os.CreateTemp("./", "runtime-env-*.yaml")
	require.NoError(t, err)

	defer os.Remove(runtimeEnvFile.Name())

	runTimeEnv := `pip:
  - requests==2.26.0
  - pendulum==2.1.2
env_vars:
  counter_name: "test_counter"
working_dir: /fake/dir/ray_working_dir/
`
	_, err = runtimeEnvFile.Write([]byte(runTimeEnv))
	require.NoError(t, err)

	runtimeEnvActual, err := runtimeEnvHasWorkingDir(filepath.Join("./", runtimeEnvFile.Name()))
	require.NoError(t, err)

	assert.NotEmpty(t, runtimeEnvActual)
	assert.Equal(t, "/fake/dir/ray_working_dir/", runtimeEnvActual)
}

func TestRaySubmitCmd(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))
	fakeSubmitJobOptions := NewJobSubmitOptions(cmdFactory, testStreams)

	fakeSubmitJobOptions.runtimeEnv = "/fake-runtime/path"
	fakeSubmitJobOptions.runtimeEnvJson = "{\"env_vars\":{\"counter_name\":\"test_counter\"}"
	fakeSubmitJobOptions.submissionID = "fake-submission-id12345"
	fakeSubmitJobOptions.entryPointCPU = 2.0
	fakeSubmitJobOptions.entryPointGPU = 1.0
	fakeSubmitJobOptions.entryPointMemory = 600
	fakeSubmitJobOptions.entryPointResource = "{\"fake-resource\":{\"the-fake-resource\"}"
	fakeSubmitJobOptions.noWait = true
	fakeSubmitJobOptions.headers = "{\"requestHeaders\": {\"header\": \"header\"}}"
	fakeSubmitJobOptions.verify = "True"
	fakeSubmitJobOptions.workingDir = "/fake/working/dir"
	fakeSubmitJobOptions.entryPoint = "python fake_python_script.py"

	actualCmd, err := fakeSubmitJobOptions.raySubmitCmd()
	require.NoError(t, err)
	expectedCmd := []string{
		"ray",
		"job",
		"submit",
		"--address",
		dashboardAddr,
		"--runtime-env",
		"/fake-runtime/path",
		"--runtime-env-json",
		"{\"env_vars\":{\"counter_name\":\"test_counter\"}",
		"--submission-id",
		"fake-submission-id12345",
		"--entrypoint-num-cpus",
		"2.000000",
		"--entrypoint-num-gpus",
		"1.000000",
		"--entrypoint-memory",
		"600",
		"--entrypoint-resource",
		"{\"fake-resource\":{\"the-fake-resource\"}",
		"--no-wait",
		"--headers",
		"{\"requestHeaders\": {\"header\": \"header\"}}",
		"--verify",
		"True",
		"--working-dir",
		"/fake/working/dir",
		"--",
		"python",
		"fake_python_script.py",
	}

	assert.Equal(t, expectedCmd, actualCmd)
}
