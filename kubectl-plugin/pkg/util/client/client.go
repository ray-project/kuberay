package client

import (
	"context"
	"fmt"
	"strings"

	dockerparser "github.com/novln/docker-parser"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
)

type Client interface {
	KubernetesClient() kubernetes.Interface
	RayClient() rayclient.Interface
	// GetRayHeadSvcName retrieves the name of RayHead service for the given RayCluster, RayJob, or RayService.
	GetRayHeadSvcName(ctx context.Context, namespace string, resourceType util.ResourceType, name string) (string, error)
	GetKubeRayOperatorVersion(ctx context.Context) (string, error)
}

type k8sClient struct {
	kubeClient kubernetes.Interface
	rayClient  rayclient.Interface
}

func NewClient(factory cmdutil.Factory) (Client, error) {
	kubeClient, err := factory.KubernetesClientSet()
	if err != nil {
		return nil, err
	}
	restConfig, err := factory.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	rayClient, err := rayclient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &k8sClient{
		kubeClient: kubeClient,
		rayClient:  rayClient,
	}, nil
}

func NewClientForTesting(kubeClient kubernetes.Interface, rayClient rayclient.Interface) Client {
	return &k8sClient{
		kubeClient: kubeClient,
		rayClient:  rayClient,
	}
}

func (c *k8sClient) KubernetesClient() kubernetes.Interface {
	return c.kubeClient
}

func (c *k8sClient) RayClient() rayclient.Interface {
	return c.rayClient
}

func (c *k8sClient) GetKubeRayOperatorVersion(ctx context.Context) (string, error) {
	deployment, err := c.kubeClient.AppsV1().Deployments("").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name in (kuberay-operator,kuberay)",
	})
	if err != nil {
		return "", fmt.Errorf("failed to get KubeRay operator deployment: %w", err)
	}

	if len(deployment.Items) == 0 {
		return "", fmt.Errorf("no KubeRay operator deployments found in any namespace")
	}

	containers := deployment.Items[0].Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return "", fmt.Errorf("no containers found in KubeRay operator deployment")
	}

	image := containers[0].Image
	ref, err := dockerparser.Parse(image)
	if err != nil {
		return "", fmt.Errorf("unable to parse KubeRay operator version from image: %w", err)
	}

	// If image reference contains both digest and tag, return both
	if strings.Contains(image, "@sha256:") && strings.Count(image, ":") == 2 {
		parts := strings.SplitN(image, ":", 2)
		return parts[len(parts)-1], nil
	}

	return ref.Tag(), nil
}

func (c *k8sClient) GetRayHeadSvcName(ctx context.Context, namespace string, resourceType util.ResourceType, name string) (string, error) {
	switch resourceType {
	case util.RayCluster:
		return c.getRayHeadSvcNameByRayCluster(ctx, namespace, name)
	case util.RayJob:
		return c.getRayHeadSvcNameByRayJob(ctx, namespace, name)
	case util.RayService:
		return c.getRayHeadSvcNameByRayService(ctx, namespace, name)
	default:
		return "", fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

func (c *k8sClient) getRayHeadSvcNameByRayCluster(ctx context.Context, namespace string, name string) (string, error) {
	rayCluster, err := c.RayClient().RayV1().RayClusters(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to find RayCluster %s: %w", name, err)
	}
	svcName := rayCluster.Status.Head.ServiceName
	return svcName, nil
}

func (c *k8sClient) getRayHeadSvcNameByRayJob(ctx context.Context, namespace string, name string) (string, error) {
	rayJob, err := c.RayClient().RayV1().RayJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to find RayJob %s: %w", name, err)
	}
	svcName := rayJob.Status.RayClusterStatus.Head.ServiceName
	return svcName, nil
}

// There are 3 services associated with a RayService:
// - <rayservice-name>-head-svc
// - <rayservice-name>-serve-svc
// - <raycluster-name>-head-svc
// This function retrieves the name of the <raycluster-name>-head-svc service.
// Actually there is no difference between which service to use, because kubectl port-forward source code first tries to find the underlying pod.
// See https://github.com/kubernetes/kubectl/blob/262825a8a665c7cae467dfaa42b63be5a5b8e5a2/pkg/cmd/portforward/portforward.go#L345 for details.
func (c *k8sClient) getRayHeadSvcNameByRayService(ctx context.Context, namespace string, name string) (string, error) {
	rayService, err := c.RayClient().RayV1().RayServices(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to find RayService %s: %w", name, err)
	}
	svcName := rayService.Status.ActiveServiceStatus.RayClusterStatus.Head.ServiceName
	return svcName, nil
}
