package version

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"

	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
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

type fakeClient struct {
	err                 error
	kuberayImageVersion string
}

func (c fakeClient) GetKubeRayOperatorVersion(_ context.Context) (string, error) {
	return c.kuberayImageVersion, c.err
}

func (c fakeClient) GetRayHeadSvcName(_ context.Context, _ string, _ util.ResourceType, _ string) (string, error) {
	return "", nil
}

func (c fakeClient) WaitRayClusterProvisioned(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (c fakeClient) KubernetesClient() kubernetes.Interface {
	return nil
}

func (c fakeClient) RayClient() rayclient.Interface {
	return nil
}

func (c fakeClient) WaitRayJobDeletionPolicyEnabled(_ context.Context, _, _ string, _ time.Time, _ time.Duration) error {
	return nil
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
			fClient := fakeClient{kuberayImageVersion: tc.kuberayImageVersion, err: tc.getKubeRayOperatorVersionError}

			var buf bytes.Buffer
			err := fakeVersionOptions.Run(context.Background(), fClient, &buf)
			require.NoError(t, err)

			assert.Equal(t, tc.expected, buf.String())
		})
	}
}
