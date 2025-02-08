package version

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"

	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
)

func TestRayVersionCheckContext(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	testContext := "test-context"

	kubeConfigWithCurrentContext, err := util.CreateTempKubeConfigFile(t, "my-fake-context")
	require.NoError(t, err)

	kubeConfigWithoutCurrentContext, err := util.CreateTempKubeConfigFile(t, "")
	require.NoError(t, err)

	tests := []struct {
		name        string
		opts        *VersionOptions
		expectError string
	}{
		{
			name: "Test validation when no context is set",
			opts: &VersionOptions{
				configFlags: genericclioptions.NewConfigFlags(false),
				ioStreams:   &testStreams,
			},
			expectError: "no context is currently set, use \"--context\" or \"kubectl config use-context <context>\" to select a new one",
		},
		{
			name: "no error when kubeconfig has current context and --context switch isn't set",
			opts: &VersionOptions{
				configFlags: &genericclioptions.ConfigFlags{
					KubeConfig: &kubeConfigWithCurrentContext,
				},
				ioStreams: &testStreams,
			},
		},
		{
			name: "no error when kubeconfig has no current context and --context switch is set",
			opts: &VersionOptions{
				configFlags: &genericclioptions.ConfigFlags{
					KubeConfig: &kubeConfigWithoutCurrentContext,
					Context:    &testContext,
				},
				ioStreams: &testStreams,
			},
		},
		{
			name: "no error when kubeconfig has current context and --context switch is set",
			opts: &VersionOptions{
				configFlags: &genericclioptions.ConfigFlags{
					KubeConfig: &kubeConfigWithCurrentContext,
					Context:    &testContext,
				},
				ioStreams: &testStreams,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.checkContext()
			fmt.Printf("err: %v\n", err)
			if tc.expectError != "" {
				assert.Equal(t, tc.expectError, err.Error())
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

func (c fakeClient) KubernetesClient() kubernetes.Interface {
	return nil
}

func (c fakeClient) RayClient() rayclient.Interface {
	return nil
}

// Tests the Run() step of the command and checks the output.
func TestRayVersionRun(t *testing.T) {
	testContext := "test-context"
	tf := cmdtesting.NewTestFactory().WithNamespace("test")
	defer tf.Cleanup()

	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()

	fakeVersionOptions := &VersionOptions{
		configFlags: &genericclioptions.ConfigFlags{
			Context: &testContext,
		},
		ioStreams: &testStreams,
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
