package version

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	clientfake "github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestRayVersionCheckContext(t *testing.T) {
	tests := []struct {
		name        string
		opts        *VersionOptions
		expectError string
	}{
		{
			name: "should error when no K8s context is set",
			opts: &VersionOptions{
				configFlags:   genericclioptions.NewConfigFlags(true),
				kubeContexter: util.NewMockKubeContexter(false),
			},
			expectError: "no context is currently set, use \"--context\" or \"kubectl config use-context <context>\" to select a new one",
		},
		{
			name: "should not error when K8s context is set",
			opts: &VersionOptions{
				configFlags:   genericclioptions.NewConfigFlags(true),
				kubeContexter: util.NewMockKubeContexter(true),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.checkContext()
			fmt.Printf("err: %v\n", err)
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Tests the Run() step of the command and checks the output.
func TestRayVersionRun(t *testing.T) {
	fakeVersionOptions := &VersionOptions{
		configFlags:   genericclioptions.NewConfigFlags(true),
		kubeContexter: util.NewMockKubeContexter(true),
	}

	tests := []struct {
		name                           string
		kuberayImageVersion            string
		getKubeRayOperatorVersionError error
		expected                       string
	}{
		{
			name:                           "Test when we can successfully get the KubeRay operator image version",
			kuberayImageVersion:            "v0.0.1",
			getKubeRayOperatorVersionError: nil,
			expected: fmt.Sprintf("kubectl ray plugin version: %s\n"+
				"KubeRay operator version: %s\n", Version, "v0.0.1"),
		},
		{
			name:                           "Test when we cannot get the KubeRay operator image version",
			kuberayImageVersion:            "",
			getKubeRayOperatorVersionError: fmt.Errorf("something went wrong"),
			expected: fmt.Sprintf("kubectl ray plugin version: %s\n"+
				"warning: KubeRay operator installation cannot be found: something went wrong. "+
				"Did you install it with the name \"kuberay-operator\"?\n", Version),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := clientfake.NewFakeClient().WithKubeRayImageVersion(tc.kuberayImageVersion).WithKubeRayOperatorVersionError(tc.getKubeRayOperatorVersionError)

			var buf bytes.Buffer
			err := fakeVersionOptions.Run(context.Background(), client, &buf)
			require.NoError(t, err)

			assert.Equal(t, tc.expected, buf.String())
		})
	}
}
