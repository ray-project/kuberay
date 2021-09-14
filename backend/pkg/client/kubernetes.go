package client

import (
	"path/filepath"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/cenkalti/backoff"
	"github.com/pkg/errors"
	"github.com/ray-project/kuberay/backend/pkg/util"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type KubernetesClientInterface interface {
	PodClient(namespace string) v1.PodInterface
	ConfigMapClient(namespace string) v1.ConfigMapInterface
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

// CreateKubernetesCoreOrFatal creates a new client for the Kubernetes pod.
func CreateKubernetesCoreOrFatal(initConnectionTimeout time.Duration, options util.ClientOptions) KubernetesClientInterface {
	var client KubernetesClientInterface
	var err error
	var operation = func() error {
		// In cluster
		kubernetesClient, err := newInClusterKubernetesClient(options)
		if err != nil {
			return err
		}
		client = kubernetesClient
		return nil
	}

	// Out of cluster
	kubernetesClient, err := newOutOfClusterKubernetesClient()
	if err == nil {
		return kubernetesClient
	}
	klog.Infof("(Expected when in cluster) Failed to create Kubernetes client by out of cluster kubeconfig. Error: %v", err)

	klog.Infof("Starting to create Kubernetes client by in cluster config.")
	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = initConnectionTimeout
	if err := backoff.Retry(operation, b); err != nil {
		klog.Fatalf("Failed to create pod client. Error: %v", err)
	}

	return client
}

func newInClusterKubernetesClient(options util.ClientOptions) (*KubernetesClient, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to initialize kubernetes client.")
	}
	restConfig.QPS = options.QPS
	restConfig.Burst = options.Burst

	clientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to initialize kubernetes client set.")
	}
	return &KubernetesClient{clientSet.CoreV1()}, nil
}

func newOutOfClusterKubernetesClient() (*KubernetesClient, error) {
	home := homedir.HomeDir()
	if home == "" {
		return nil, errors.New("Cannot get home dir")
	}

	defaultKubeConfigPath := filepath.Join(home, ".kube", "config")
	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", defaultKubeConfigPath)
	if err != nil {
		return nil, err
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to initialize kubernetes clientSet set.")
	}
	return &KubernetesClient{clientSet.CoreV1()}, nil
}
