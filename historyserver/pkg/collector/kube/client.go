package kube

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type KubeClient struct {
	Client client.Client
}

func NewKubeClient(c client.Client) *KubeClient {
	return &KubeClient{Client: c}
}
