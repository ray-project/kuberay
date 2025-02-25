package create

import (
	"context"
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/utils/ptr"

	kubefake "k8s.io/client-go/kubernetes/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayClientFake "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/fake"
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

func TestRayClusterCreateClusterRun(t *testing.T) {
	namespace := "namespace-1"
	clusterName := "cluster-1"

	options := CreateClusterOptions{
		configFlags: &genericclioptions.ConfigFlags{
			Namespace: ptr.To(namespace),
		},
		kubeContexter: util.NewMockKubeContexter(true),
		clusterName:   clusterName,
	}

	t.Run("should error when the Ray cluster already exists", func(t *testing.T) {
		rayClusters := []runtime.Object{
			&rayv1.RayCluster{
				ObjectMeta: v1.ObjectMeta{
					Namespace: namespace,
					Name:      clusterName,
				},
				Spec: rayv1.RayClusterSpec{},
			},
		}

		rayClient := rayClientFake.NewSimpleClientset(rayClusters...)
		k8sClients := client.NewClientForTesting(kubefake.NewClientset(), rayClient)

		err := options.Run(context.Background(), k8sClients)
		require.Error(t, err)
	})
}
