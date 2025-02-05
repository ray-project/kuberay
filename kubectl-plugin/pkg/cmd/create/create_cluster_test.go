package create

import (
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestRayCreateClusterComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	fakeCreateClusterOptions := NewCreateClusterOptions(testStreams)
	fakeArgs := []string{"testRayClusterName"}
	cmd := &cobra.Command{Use: "cluster"}

	err := fakeCreateClusterOptions.Complete(cmd, fakeArgs)
	require.NoError(t, err)
	assert.Equal(t, "default", *fakeCreateClusterOptions.configFlags.Namespace)
	assert.Equal(t, "testRayClusterName", fakeCreateClusterOptions.clusterName)
}

func TestRayCreateClusterValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	testNS, testContext, testBT, testImpersonate := "test-namespace", "test-context", "test-bearer-token", "test-person"

	kubeConfigWithCurrentContext, err := util.CreateTempKubeConfigFile(t, testContext)
	require.NoError(t, err)

	kubeConfigWithoutCurrentContext, err := util.CreateTempKubeConfigFile(t, "")
	require.NoError(t, err)

	tests := []struct {
		name        string
		opts        *CreateClusterOptions
		expectError string
	}{
		{
			name: "Test validation when no context is set",
			opts: &CreateClusterOptions{
				configFlags: &genericclioptions.ConfigFlags{
					KubeConfig: &kubeConfigWithoutCurrentContext,
				},
				ioStreams: &testStreams,
			},
			expectError: "no context is currently set, use \"--context\" or \"kubectl config use-context <context>\" to select a new one",
		},
		{
			name: "no error when kubeconfig has current context and --context switch isn't set",
			opts: &CreateClusterOptions{
				configFlags: &genericclioptions.ConfigFlags{
					KubeConfig: &kubeConfigWithCurrentContext,
				},
				ioStreams: &testStreams,
			},
		},
		{
			name: "no error when kubeconfig has no current context and --context switch is set",
			opts: &CreateClusterOptions{
				configFlags: &genericclioptions.ConfigFlags{
					KubeConfig: &kubeConfigWithoutCurrentContext,
					Context:    &testContext,
				},
				ioStreams: &testStreams,
			},
		},
		{
			name: "no error when kubeconfig has current context and --context switch is set",
			opts: &CreateClusterOptions{
				configFlags: &genericclioptions.ConfigFlags{
					KubeConfig: &kubeConfigWithCurrentContext,
					Context:    &testContext,
				},
				ioStreams: &testStreams,
			},
		},
		{
			name: "Successful submit job validation with RayJob",
			opts: &CreateClusterOptions{
				configFlags: &genericclioptions.ConfigFlags{
					Namespace:        &testNS,
					Context:          &testContext,
					KubeConfig:       &kubeConfigWithCurrentContext,
					BearerToken:      &testBT,
					Impersonate:      &testImpersonate,
					ImpersonateGroup: &[]string{"fake-group"},
				},
				ioStreams:      &testStreams,
				clusterName:    "fakeclustername",
				rayVersion:     "ray-version",
				image:          "ray-image",
				headCPU:        "5",
				headGPU:        "1",
				headMemory:     "5Gi",
				workerReplicas: 3,
				workerCPU:      "4",
				workerMemory:   "5Gi",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectError != "" {
				require.Error(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
