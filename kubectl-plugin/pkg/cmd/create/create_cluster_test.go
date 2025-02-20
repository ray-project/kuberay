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
	tests := []struct {
		name        string
		opts        *CreateClusterOptions
		expectError string
	}{
		{
			name: "should error when no K8s context is set",
			opts: &CreateClusterOptions{
				configFlags:   genericclioptions.NewConfigFlags(true),
				kubeContexter: util.NewMockKubeContexter(false),
			},
			expectError: "no context is currently set, use \"--context\" or \"kubectl config use-context <context>\" to select a new one",
		},
		{
			name: "should not error when K8s context is set",
			opts: &CreateClusterOptions{
				configFlags:   genericclioptions.NewConfigFlags(true),
				kubeContexter: util.NewMockKubeContexter(true),
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
