package job

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestRayJobSubmitComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	fakeSubmitJobOptions := NewJobSubmitOptions(testStreams)
	fakeSubmitJobOptions.runtimeEnv = "././fake/path/to/env/yaml"
	fakeSubmitJobOptions.fileName = "fake/path/to/rayjob.yaml"

	err := fakeSubmitJobOptions.Complete()
	assert.Equal(t, "default", *fakeSubmitJobOptions.configFlags.Namespace)
	assert.Nil(t, err)
	assert.Equal(t, "fake/path/to/env/yaml", fakeSubmitJobOptions.runtimeEnv)
}

func TestRayJobSubmitValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()

	testNS, testContext, testBT, testImpersonate := "test-namespace", "test-contet", "test-bearer-token", "test-person"

	// Fake directory for kubeconfig
	fakeDir, err := os.MkdirTemp("", "fake-dir")
	assert.Nil(t, err)
	defer os.RemoveAll(fakeDir)

	// Set up fake config for kubeconfig
	config := &api.Config{
		Clusters: map[string]*api.Cluster{
			"test-cluster": {
				Server:                "https://fake-kubernetes-cluster.example.com",
				InsecureSkipTLSVerify: true, // For testing purposes
			},
		},
		Contexts: map[string]*api.Context{
			"my-fake-context": {
				Cluster:  "my-fake-cluster",
				AuthInfo: "my-fake-user",
			},
		},
		CurrentContext: "my-fake-context",
		AuthInfos: map[string]*api.AuthInfo{
			"my-fake-user": {
				Token: "", // Empty for testing without authentication
			},
		},
	}

	fakeFile := filepath.Join(fakeDir, ".kubeconfig")

	err = clientcmd.WriteToFile(*config, fakeFile)
	assert.Nil(t, err)

	fakeConfigFlags := &genericclioptions.ConfigFlags{
		Namespace:        &testNS,
		Context:          &testContext,
		KubeConfig:       &fakeFile,
		BearerToken:      &testBT,
		Impersonate:      &testImpersonate,
		ImpersonateGroup: &[]string{"fake-group"},
	}

	rayYaml := `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`

	rayJobYamlPath := filepath.Join(fakeDir, "rayjob-temp-*.yaml")

	file, err := os.Create(rayJobYamlPath)
	assert.Nil(t, err)
	defer file.Close()

	_, err = file.Write([]byte(rayYaml))
	assert.Nil(t, err)

	tests := []struct {
		name        string
		opts        *SubmitJobOptions
		expectError string
	}{
		{
			name: "Test validation when no context is set",
			opts: &SubmitJobOptions{
				configFlags: genericclioptions.NewConfigFlags(false),
				ioStreams:   &testStreams,
			},
			expectError: "no context is currently set, use \"kubectl config use-context <context>\" to select a new one",
		},
		{
			name: "Successful submit job validation with RayJob",
			opts: &SubmitJobOptions{
				configFlags: fakeConfigFlags,
				ioStreams:   &testStreams,
				fileName:    rayJobYamlPath,
				workingDir:  "Fake/File/Path",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectError != "" {
				assert.Equal(t, tc.expectError, err.Error())
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestDecodeRayJobYaml(t *testing.T) {
	rayjobtmpfile, err := os.CreateTemp("./", "rayjob-temp-*.yaml")
	assert.Nil(t, err)

	defer os.Remove(rayjobtmpfile.Name())

	rayYaml := `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`
	_, err = rayjobtmpfile.Write([]byte(rayYaml))
	assert.Nil(t, err)

	rayJobYamlActual, err := decodeRayJobYaml(filepath.Join("./", rayjobtmpfile.Name()))
	assert.Nil(t, err)

	assert.Equal(t, "rayjob-sample", rayJobYamlActual.GetName())

	submissionMode := rayJobYamlActual.Spec.SubmissionMode
	assert.Equal(t, rayv1.InteractiveMode, submissionMode)
}

func TestRuntimeEnvHasWorkingDir(t *testing.T) {
	runtimeEnvFile, err := os.CreateTemp("./", "runtime-env-*.yaml")
	assert.Nil(t, err)

	defer os.Remove(runtimeEnvFile.Name())

	runTimeEnv := `pip:
  - requests==2.26.0
  - pendulum==2.1.2
env_vars:
  counter_name: "test_counter"
working_dir: /fake/dir/ray_working_dir/
`
	_, err = runtimeEnvFile.Write([]byte(runTimeEnv))
	assert.Nil(t, err)

	runtimeEnvActual, err := runtimeEnvHasWorkingDir(filepath.Join("./", runtimeEnvFile.Name()))
	assert.Nil(t, err)

	assert.NotEmpty(t, runtimeEnvActual)
	assert.Equal(t, runtimeEnvActual, "/fake/dir/ray_working_dir/")
}

func TestRaySubmitCmd(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	fakeSubmitJobOptions := NewJobSubmitOptions(testStreams)

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
	assert.Nil(t, err)
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
