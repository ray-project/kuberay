package create

import (
	"context"
	"fmt"
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	kubefake "k8s.io/client-go/kubernetes/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayClientFake "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/fake"
)

func TestRayCreateClusterComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))
	fakeCreateClusterOptions := NewCreateClusterOptions(cmdFactory, testStreams)
	fakeArgs := []string{"testRayClusterName"}
	cmd := &cobra.Command{Use: "cluster"}
	cmd.Flags().StringVarP(&fakeCreateClusterOptions.namespace, "namespace", "n", "", "")

	err := fakeCreateClusterOptions.Complete(cmd, fakeArgs)
	require.NoError(t, err)
	assert.Equal(t, "default", fakeCreateClusterOptions.namespace)
	assert.Equal(t, "testRayClusterName", fakeCreateClusterOptions.clusterName)
}

func TestRayCreateClusterValidate(t *testing.T) {
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	tests := []struct {
		name        string
		opts        *CreateClusterOptions
		expectError string
	}{
		{
			name: "should error when a resource quantity is invalid",
			opts: &CreateClusterOptions{
				cmdFactory: cmdFactory,
				headCPU:    "1",
				headMemory: "softmax",
			},
			expectError: "head-memory is not a valid resource quantity: quantities must match the regular expression '^([+-]?[0-9.]+)([eEinumkKMGTP]*[-+]?[0-9]*)$'",
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
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	options := CreateClusterOptions{
		cmdFactory:   cmdFactory,
		clusterName:  clusterName,
		labels:       map[string]string{"app": "ray", "env": "dev"},
		annotations:  map[string]string{"ttl-hours": "24", "owner": "chthulu"},
		headCPU:      "1",
		headMemory:   "1Gi",
		headGPU:      "0",
		workerCPU:    "1",
		workerMemory: "1Gi",
		workerGPU:    "1",
		workerTPU:    "0",
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

func TestNewCreateClusterCommand(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmd := NewCreateClusterCommand(cmdutil.NewFactory(genericclioptions.NewConfigFlags(true)), testStreams)
	cmd.Flags().StringP("namespace", "n", "", "")

	workerNodeSelectors := fmt.Sprintf(
		"app=ray,env=dev,%s=tpu-v5,%s=2x4",
		util.NodeSelectorGKETPUAccelerator,
		util.NodeSelectorGKETPUTopology,
	)

	cmd.SetArgs([]string{
		"sample-cluster",
		"--ray-version", "2.44.0",
		"--image", "rayproject/ray:2.44.0",
		"--head-cpu", "1",
		"--head-memory", "5Gi",
		"--head-gpu", "1",
		"--head-ephemeral-storage", "10Gi",
		"--head-ray-start-params", "metrics-export-port=8080,num-cpus=2",
		"--head-node-selectors", "app=ray,env=dev",
		"--worker-replicas", "3",
		"--worker-cpu", "1",
		"--worker-memory", "5Gi",
		"--worker-gpu", "1",
		"--worker-tpu", "1",
		"--worker-ephemeral-storage", "10Gi",
		"--worker-ray-start-params", "metrics-export-port=8081,num-cpus=2",
		"--worker-node-selectors", workerNodeSelectors,
		"--labels", "app=ray,env=dev",
		"--annotations", "ttl-hours=24,owner=chthulu",
		"--dry-run",
		"--wait",
		"--timeout", "10s",
	})
	require.NoError(t, cmd.Execute())
}
