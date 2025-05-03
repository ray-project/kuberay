package utils

import (
	"context"

	"k8s.io/apimachinery/pkg/labels"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apisrayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/pkg/client/listers/ray/v1"
)

// Implements RayClusterLister interface with controller-runtime client
type RayclusterLister struct {
	Client ctrlclient.Client
}

func NewRayclusterLister(client ctrlclient.Client) rayv1.RayClusterLister {
	return &RayclusterLister{Client: client}
}

func (l *RayclusterLister) List(_ labels.Selector) (ret []*apisrayv1.RayCluster, err error) {
	return clientListRayClusters(l.Client, ctrlclient.ListOptions{})
}

// RayClusters returns an object that can list and get RayClusters.
func (l *RayclusterLister) RayClusters(namespace string) rayv1.RayClusterNamespaceLister {
	return &rayClusterNamespaceLister{
		Client:    l.Client,
		Namespace: namespace,
	}
}

// Implements RayClusterNamespaceLister
type rayClusterNamespaceLister struct {
	Client    ctrlclient.Client
	Namespace string
}

func (n *rayClusterNamespaceLister) List(_ labels.Selector) (ret []*apisrayv1.RayCluster, err error) {
	return clientListRayClusters(n.Client, ctrlclient.ListOptions{Namespace: n.Namespace})
}

func (n *rayClusterNamespaceLister) Get(name string) (*apisrayv1.RayCluster, error) {
	rayCluster := apisrayv1.RayCluster{}
	err := n.Client.Get(context.TODO(), ctrlclient.ObjectKey{Namespace: n.Namespace, Name: name}, &rayCluster)
	return &rayCluster, err
}

func clientListRayClusters(client ctrlclient.Client, listOptions ctrlclient.ListOptions) (ret []*apisrayv1.RayCluster, err error) {
	var rayClusterList apisrayv1.RayClusterList
	var results []*apisrayv1.RayCluster

	err = client.List(context.TODO(), &rayClusterList, &listOptions)

	if err == nil {
		for _, rayCluster := range rayClusterList.Items {
			results = append(results, rayCluster.DeepCopy())
		}
	}

	return results, err
}
