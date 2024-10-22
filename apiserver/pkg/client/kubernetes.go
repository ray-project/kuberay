package client

import (
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/ray-project/kuberay/apiserver/pkg/util"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type KubernetesClientInterface interface {
	PodClient(namespace string) v1.PodInterface
	ConfigMapClient(namespace string) v1.ConfigMapInterface
	NamespaceClient() v1.NamespaceInterface
	EventsClient(namespace string) v1.EventInterface
}

type KubernetesClient struct {
	coreV1Client v1.CoreV1Interface
}

func (c *KubernetesClient) PodClient(namespace string) v1.PodInterface {
	return c.coreV1Client.Pods(namespace)
}

func (c *KubernetesClient) ConfigMapClient(namespace string) v1.ConfigMapInterface {
	return c.coreV1Client.ConfigMaps(namespace)
}

func (c *KubernetesClient) EventsClient(namespace string) v1.EventInterface {
	return c.coreV1Client.Events(namespace)
}

func (c *KubernetesClient) NamespaceClient() v1.NamespaceInterface {
	return c.coreV1Client.Namespaces()
}

// CreateKubernetesCoreOrFatal creates a new client for the Kubernetes pod.
func CreateKubernetesCoreOrFatal(options util.ClientOptions) KubernetesClientInterface {
	cfg, err := config.GetConfig()
	if err != nil {
		klog.Fatalf("Failed to create TokenReview client. Error: %v", err)
	}
	cfg.QPS = options.QPS
	cfg.Burst = options.Burst

	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Failed to create pod client. Error: %v", err)
	}
	return &KubernetesClient{clientSet.CoreV1()}
}
