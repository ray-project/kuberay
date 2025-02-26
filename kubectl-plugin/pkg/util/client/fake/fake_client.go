package fake

import (
	"context"
	"time"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"k8s.io/client-go/kubernetes"

	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
)

type FakeClient struct {
	Err                 error
	KuberayImageVersion string
}

func (c FakeClient) GetKubeRayOperatorVersion(_ context.Context) (string, error) {
	return c.KuberayImageVersion, c.Err
}

func (c FakeClient) GetRayHeadSvcName(_ context.Context, _ string, _ util.ResourceType, _ string) (string, error) {
	return "", nil
}

func (c FakeClient) WaitRayClusterProvisioned(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (c FakeClient) KubernetesClient() kubernetes.Interface {
	return nil
}

func (c FakeClient) RayClient() rayclient.Interface {
	return nil
}
