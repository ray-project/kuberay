package util

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd/api"
)

// KubeContexter is an interface that checks if the kubeconfig has a current context or if the --context switch is set.
type KubeContexter interface {
	HasContext(config api.Config, configFlags *genericclioptions.ConfigFlags) bool
}

type DefaultKubeContexter struct{}

// HasKubectlContext checks if the kubeconfig has a current context or if the --context switch is set.
func (d *DefaultKubeContexter) HasContext(config api.Config, configFlags *genericclioptions.ConfigFlags) bool {
	return config.CurrentContext != "" || (configFlags.Context != nil && *configFlags.Context != "")
}

// MockKubeContexter is a mock implementation of KubeContexter that should be used only in tests.
type MockKubeContexter struct {
	returnValue bool
}

func (m *MockKubeContexter) HasContext(_ api.Config, _ *genericclioptions.ConfigFlags) bool {
	return m.returnValue
}

func NewMockKubeContexter(returnValue bool) *MockKubeContexter {
	return &MockKubeContexter{returnValue: returnValue}
}
