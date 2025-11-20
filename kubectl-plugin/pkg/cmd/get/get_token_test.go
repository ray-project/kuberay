package get

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kubefake "k8s.io/client-go/kubernetes/fake"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayClientFake "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/fake"
)

// Tests the Run() step of the command and ensure that the output is as expected.
func TestTokenGetRun(t *testing.T) {
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	testStreams, _, resBuf, _ := genericclioptions.NewTestIOStreams()
	fakeTokenGetOptions := NewGetTokenOptions(cmdFactory, testStreams)

	rayCluster := &rayv1.RayCluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      "raycluster-kuberay",
			Namespace: "test",
		},
		Spec: rayv1.RayClusterSpec{
			AuthOptions: &rayv1.AuthOptions{
				Mode: rayv1.AuthModeToken,
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      "raycluster-kuberay",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"auth_token": []byte("token"),
		},
	}

	kubeClientSet := kubefake.NewClientset(secret)
	rayClient := rayClientFake.NewSimpleClientset(rayCluster)
	k8sClients := client.NewClientForTesting(kubeClientSet, rayClient)

	cmd := &cobra.Command{}
	cmd.Flags().StringVarP(&fakeTokenGetOptions.namespace, "namespace", "n", secret.Namespace, "")
	err := fakeTokenGetOptions.Complete([]string{rayCluster.Name}, cmd)
	require.NoError(t, err)
	err = fakeTokenGetOptions.Run(t.Context(), k8sClients)
	require.NoError(t, err)

	assert.Equal(t, secret.Data["auth_token"], resBuf.Bytes())
}
