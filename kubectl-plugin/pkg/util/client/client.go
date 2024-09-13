package client

import (
	"context"
	"fmt"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type Client interface {
	KubernetesClient() kubernetes.Interface
	DynamicClient() dynamic.Interface
	// GetRayHeadSvcName retrieves the name of RayHead service for the given RayCluster, RayJob, or RayService.
	GetRayHeadSvcName(ctx context.Context, namespace string, resourceType util.ResourceType, name string) (string, error)
}

type k8sClient struct {
	kubeClient    kubernetes.Interface
	dynamicClient dynamic.Interface
}

func NewClient(factory cmdutil.Factory) (Client, error) {
	kubeClient, err := factory.KubernetesClientSet()
	if err != nil {
		return nil, err
	}
	dynamicClient, err := factory.DynamicClient()
	if err != nil {
		return nil, err
	}
	return &k8sClient{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
	}, nil
}

func NewClientForTesting(kubeClient kubernetes.Interface, dynamicClient dynamic.Interface) Client {
	return &k8sClient{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
	}
}

func (c *k8sClient) KubernetesClient() kubernetes.Interface {
	return c.kubeClient
}

func (c *k8sClient) DynamicClient() dynamic.Interface {
	return c.dynamicClient
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
	rayCluster, err := c.DynamicClient().Resource(util.RayClusterGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to find RayCluster %s: %w", name, err)
	}
	svcName, err := extractRayHeadSvcNameFromRayClusterStatus(rayCluster.Object["status"])
	if err != nil {
		return "", fmt.Errorf("unable to extract RayHead service name from RayCluster %s: %w", name, err)
	}
	return svcName, nil
}

func (c *k8sClient) getRayHeadSvcNameByRayJob(ctx context.Context, namespace string, name string) (string, error) {
	rayJob, err := c.DynamicClient().Resource(util.RayJobGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to find RayJob %s: %w", name, err)
	}
	status := rayJob.Object["status"]
	rayClusterStatus, ok := status.(map[string]interface{})["rayClusterStatus"]
	if !ok {
		return "", fmt.Errorf("unable to find rayClusterStatus in status")
	}
	svcName, err := extractRayHeadSvcNameFromRayClusterStatus(rayClusterStatus)
	if err != nil {
		return "", fmt.Errorf("unable to extract RayHead service name from RayJob %s: %w", name, err)
	}
	return svcName, nil
}

func (c *k8sClient) getRayHeadSvcNameByRayService(ctx context.Context, namespace string, name string) (string, error) {
	rayService, err := c.DynamicClient().Resource(util.RayServiceGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to find RayService %s: %w", name, err)
	}
	status := rayService.Object["status"]
	activeServiceStatus, ok := status.(map[string]interface{})["activeServiceStatus"]
	if !ok {
		return "", fmt.Errorf("unable to find activeServiceStatus in status")
	}
	rayClusterStatus, ok := activeServiceStatus.(map[string]interface{})["rayClusterStatus"]
	if !ok {
		return "", fmt.Errorf("unable to find rayClusterStatus in activeServiceStatus")
	}
	svcName, err := extractRayHeadSvcNameFromRayClusterStatus(rayClusterStatus)
	if err != nil {
		return "", fmt.Errorf("unable to extract RayHead service name from RayJob %s: %w", name, err)
	}
	return svcName, nil
}

func extractRayHeadSvcNameFromRayClusterStatus(status interface{}) (string, error) {
	head, ok := status.(map[string]interface{})["head"]
	if !ok {
		return "", fmt.Errorf("unable to find head in status")
	}
	svcName, ok := head.(map[string]interface{})["serviceName"].(string)
	if !ok {
		return "", fmt.Errorf("unable to find serviceName in head")
	}
	return svcName, nil
}
