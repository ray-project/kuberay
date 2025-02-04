package util

import (
	"path/filepath"
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// HasKubectlContext checks if the kubeconfig has a current context or if the --context switch is set.
func HasKubectlContext(config api.Config, configFlags *genericclioptions.ConfigFlags) bool {
	return config.CurrentContext != "" || (configFlags.Context != nil && *configFlags.Context != "")
}

// CreateTempKubeConfigFile creates a temporary kubeconfig file with the given current context.
// This function should only be used in tests.
func CreateTempKubeConfigFile(t *testing.T, currentContext string) (string, error) {
	tmpDir := t.TempDir()

	// Set up fake config for kubeconfig
	config := &api.Config{
		Clusters: map[string]*api.Cluster{
			"test-cluster": {
				Server:                "https://fake-kubernetes-cluster.example.com",
				InsecureSkipTLSVerify: true, // For testing purposes
			},
		},
		Contexts: map[string]*api.Context{
			"my-fake-context": {
				Cluster:  "my-fake-cluster",
				AuthInfo: "my-fake-user",
			},
		},
		CurrentContext: currentContext,
		AuthInfos: map[string]*api.AuthInfo{
			"my-fake-user": {
				Token: "", // Empty for testing without authentication
			},
		},
	}

	fakeFile := filepath.Join(tmpDir, ".kubeconfig")

	return fakeFile, clientcmd.WriteToFile(*config, fakeFile)
}
