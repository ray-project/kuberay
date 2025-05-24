package job

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestRayJobSubmitWithYamlValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	fakeDir := t.TempDir()

	tests := []struct {
		name        string
		yamlContent string
		expectError string
	}{
		{
			name: "Successful submit job validation with RayJob",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`,
		},
		{
			name: "BackoffLimit co-exist with InteractiveMode",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  backoffLimit: 1`,
			expectError: "BackoffLimit is incompatible with InteractiveMode",
		},
		{
			name: "BackoffLimit is set to 0 with InteractiveMode",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  backoffLimit: 0`,
		},
		{
			name: "shutdownAfterJobFinishes is false and ttlSecondsAfterFinished is not zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: false
  ttlSecondsAfterFinished: 10
  submissionMode: 'InteractiveMode'`,
			expectError: "ttlSecondsAfterFinished is only supported when shutdownAfterJobFinishes is set to true",
		},
		{
			name: "shutdownAfterJobFinishes is true and ttlSecondsAfterFinished is not zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: 10
  submissionMode: 'InteractiveMode'`,
		},
		{
			name: "ttlSecondsAfterFinished is less than zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: -10
  submissionMode: 'InteractiveMode'`,
			expectError: "ttlSecondsAfterFinished must be greater than or equal to 0",
		},
		{
			name: "ttlSecondsAfterFinished is less than zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: 0
  submissionMode: 'InteractiveMode'`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rayJobYamlPath := filepath.Join(fakeDir, "rayjob-temp-*.yaml")

			file, err := os.Create(rayJobYamlPath)
			require.NoError(t, err)
			_, err = file.Write([]byte(tc.yamlContent))
			require.NoError(t, err)

			opts := &SubmitJobOptions{
				cmdFactory: cmdFactory,
				ioStreams:  &testStreams,
				fileName:   rayJobYamlPath,
				workingDir: "Fake/File/Path",
			}

			err = opts.Validate(&cobra.Command{})
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRayJobSubmitWithoutYamlValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	test := []struct {
		name                     string
		rayjobName               string
		expectError              string
		ttlSecondsAfterFinished  int32
		shutdownAfterJobFinishes bool
	}{
		{
			name:                    "ttlSecondsAfterFinished is validate value",
			rayjobName:              "rayjob-sample",
			ttlSecondsAfterFinished: 10,
		},
		{
			name:                    "ttlSecondsAfterFinished is less than zero",
			rayjobName:              "rayjob-sample",
			ttlSecondsAfterFinished: -10,
			expectError:             "--ttl-seconds-after-finished must be greater than or equal to 0",
		},
		{
			name:                    "ttlSecondsAfterFinished is equal to zero",
			rayjobName:              "rayjob-sample",
			ttlSecondsAfterFinished: 0,
		},
	}

	for _, tc := range test {
		t.Run(tc.name, func(t *testing.T) {
			opts := &SubmitJobOptions{
				cmdFactory:              cmdFactory,
				ioStreams:               &testStreams,
				rayjobName:              tc.rayjobName,
				workingDir:              "Fake/File/Path",
				ttlSecondsAfterFinished: tc.ttlSecondsAfterFinished,
			}
			err := opts.Validate(&cobra.Command{})
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRayJobSubmitCmdFlagsOverrideYaml(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	fakeDir := t.TempDir()

	tests := []struct {
		name        string
		yamlContent string
		flagMap     map[string]any
		expectSpec  map[string]any
		expectError string
	}{
		{
			name: "Both shutdownAfterJobFinishes and ttlSecondsAfterFinished are not set in yaml",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectSpec: map[string]any{
				"ShutdownAfterJobFinishes": true,
				"TTLSecondsAfterFinished":  int32(20),
			},
		},
		{
			name: "Both shutdownAfterJobFinishes and ttlSecondsAfterFinished are set in yaml with wrong values",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: false
  ttlSecondsAfterFinished: -10
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectSpec: map[string]any{
				"ShutdownAfterJobFinishes": true,
				"TTLSecondsAfterFinished":  int32(20),
			},
		},
		{
			name: "Only shutdownAfterJobFinishes is set in yaml",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: false
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectSpec: map[string]any{
				"ShutdownAfterJobFinishes": true,
				"TTLSecondsAfterFinished":  int32(20),
			},
		},
		{
			name: "Only ttlSecondsAfterFinished is set in yaml",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  ttlSecondsAfterFinished: 10
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectSpec: map[string]any{
				"ShutdownAfterJobFinishes": true,
				"TTLSecondsAfterFinished":  int32(20),
			},
		},
		{
			name: "Override only ttlSecondsAfterFinished",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: 100
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 200,
			},
			expectSpec: map[string]any{
				"ShutdownAfterJobFinishes": true,
				"TTLSecondsAfterFinished":  int32(200),
			},
		},
		{
			name: "Set ttl-seconds-after-finished to zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 0,
			},
			expectSpec: map[string]any{
				"ShutdownAfterJobFinishes": true,
				"TTLSecondsAfterFinished":  int32(0),
			},
		},
		{
			name: "Override only ttlSecondsAfterFinished to negative and cause error",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: 10
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": -10,
			},
			expectError: "--ttl-seconds-after-finished must be greater than or equal to 0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rayJobYamlPath := filepath.Join(fakeDir, "rayjob-temp-*.yaml")

			file, err := os.Create(rayJobYamlPath)
			require.NoError(t, err)
			_, err = file.Write([]byte(tc.yamlContent))
			require.NoError(t, err)

			opts := &SubmitJobOptions{
				cmdFactory: cmdFactory,
				ioStreams:  &testStreams,
				fileName:   rayJobYamlPath,
				workingDir: "Fake/File/Path",
			}
			cmd := &cobra.Command{}
			cmd.Flags().Int32Var(&opts.ttlSecondsAfterFinished, "ttl-seconds-after-finished", 0, "")

			args := []string{}
			for flag, value := range tc.flagMap {
				if v, ok := value.(bool); ok && v {
					args = append(args, fmt.Sprintf("--%s", flag))
				} else {
					args = append(args, fmt.Sprintf("--%s=%v", flag, value))
				}
			}

			cmd.SetArgs(args)
			err = cmd.ParseFlags(args)
			require.NoError(t, err)

			err = opts.Validate(cmd)
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}

			if tc.expectSpec != nil {
				for field, expected := range tc.expectSpec {
					actual := reflect.ValueOf(opts.RayJob.Spec).FieldByName(field).Interface()
					require.Equal(t, expected, actual)
				}
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
