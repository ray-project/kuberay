package create

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestRayCreateServiceComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	fakeOptions := NewCreateServiceOptions(testStreams)
	cmd := &cobra.Command{Use: "service"}
	fakeArgs := []string{"testRayServiceName"}

	err := fakeOptions.Complete(cmd, fakeArgs)
	require.NoError(t, err)

	assert.Equal(t, "default", *fakeOptions.configFlags.Namespace)
	assert.Equal(t, "testRayServiceName", fakeOptions.rayServiceName)
}

func TestRayCreateServiceValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	testNS, testContext := "test-namespace", "test-context"

	kubeConfigWithCurrentContext, err := util.CreateTempKubeConfigFile(t, testContext)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	validServeConfig := filepath.Join(tmpDir, "serve-config.yaml")
	err = os.WriteFile(validServeConfig, []byte("serve: config"), 0o600)
	require.NoError(t, err)

	tests := []struct {
		name        string
		opts        *CreateServiceOptions
		expectError string
	}{
		{
			name: "Test validation when serveConfig file does not exist",
			opts: &CreateServiceOptions{
				configFlags: &genericclioptions.ConfigFlags{
					KubeConfig: &kubeConfigWithCurrentContext,
					Context:    &testContext,
				},
				ioStreams:   &testStreams,
				serveConfig: "non-existent-file.yaml",
			},
			expectError: "serve config file does not exist. Failed with: stat non-existent-file.yaml: no such file or directory",
		},
		{
			name: "Successful create service validation",
			opts: &CreateServiceOptions{
				configFlags: &genericclioptions.ConfigFlags{
					Namespace:  &testNS,
					KubeConfig: &kubeConfigWithCurrentContext,
				},
				ioStreams:      &testStreams,
				serveConfig:    validServeConfig,
				rayVersion:     "ray-version",
				image:          "ray-image",
				headCPU:        "2",
				headGPU:        "0",
				headMemory:     "4Gi",
				workerCPU:      "2",
				workerGPU:      "0",
				workerMemory:   "4Gi",
				workerReplicas: 1,
			},
			expectError: "",
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
