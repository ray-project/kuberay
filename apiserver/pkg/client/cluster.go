package client

import (
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/cenkalti/backoff"
	"github.com/golang/glog"
	rayclusterclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/raycluster/v1alpha1"
	"k8s.io/klog/v2"
)

type ClusterClientInterface interface {
	RayClusterClient(namespace string) rayiov1alpha1.RayClusterInterface
}

type RayClusterClient struct {
	client rayiov1alpha1.RayV1alpha1Interface
}

func (cc RayClusterClient) RayClusterClient(namespace string) rayiov1alpha1.RayClusterInterface {
	return cc.client.RayClusters(namespace)
}

func NewRayClusterClientOrFatal(initConnectionTimeout time.Duration, options util.ClientOptions) ClusterClientInterface {
	var rayClusterClient rayiov1alpha1.RayV1alpha1Interface
	var err error
	var operation = func() error {
		// in cluster
		restConfig, err := rest.InClusterConfig()
		if err != nil {
			return errors.Wrap(err, "Failed to initialize RestConfig.")
		}
		restConfig.QPS = options.QPS
		restConfig.Burst = options.Burst

		rayClusterClient = rayclusterclient.NewForConfigOrDie(restConfig).RayV1alpha1()
		return nil
	}

	// out of cluster
	rayClusterClient, err = newOutOfClusterRayClusterClient()
	if err == nil {
		return &RayClusterClient{client: rayClusterClient}
	}
	klog.Infof("(Expected when in cluster) Failed to create RayCluster client by out of cluster kubeconfig. Error: %v", err)

	klog.Infof("Starting to create RayCluster client by in cluster config.")
	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = initConnectionTimeout
	err = backoff.Retry(operation, b)

	if err != nil {
		glog.Fatalf("Failed to create TokenReview client. Error: %v", err)
	}
	return &RayClusterClient{client: rayClusterClient}
}

func newOutOfClusterRayClusterClient() (rayiov1alpha1.RayV1alpha1Interface, error) {
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
	rayClusterClient := rayclusterclient.NewForConfigOrDie(config).RayV1alpha1()
	return rayClusterClient, nil
}
