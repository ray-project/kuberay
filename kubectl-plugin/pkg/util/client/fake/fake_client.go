package fake

import (
	"context"
	"time"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"k8s.io/client-go/kubernetes"

	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
)

type FakeClient interface {
	WithKubeRayImageVersion(version string) FakeClient
	WithKubeRayOperatorVersionError(err error) FakeClient
	GetKubeRayOperatorVersion(ctx context.Context) (string, error)
	GetRayHeadSvcName(ctx context.Context, namespace string, resourceType util.ResourceType, name string) (string, error)
	WaitRayClusterProvisioned(ctx context.Context, namespace, name string, timeout time.Duration) error
	KubernetesClient() kubernetes.Interface
	RayClient() rayclient.Interface
}

type fakeClient struct {
	err                 error
	kuberayImageVersion string
}

func NewFakeClient() FakeClient {
	return &fakeClient{}
}

func (c *fakeClient) WithKubeRayImageVersion(version string) FakeClient {
	c.kuberayImageVersion = version
	return c
}

func (c *fakeClient) WithKubeRayOperatorVersionError(err error) FakeClient {
	c.err = err
	return c
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
