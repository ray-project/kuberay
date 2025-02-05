package create

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

func TestRayCreateClusterComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	fakeCreateClusterOptions := NewCreateClusterOptions(testStreams)
	fakeArgs := []string{"testRayClusterName"}
	cmd := &cobra.Command{Use: "cluster"}

	err := fakeCreateClusterOptions.Complete(cmd, fakeArgs)
	assert.NoError(t, err)
	assert.Equal(t, "default", *fakeCreateClusterOptions.configFlags.Namespace)
	assert.Equal(t, "testRayClusterName", fakeCreateClusterOptions.clusterName)
}

func TestRayCreateClusterValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()

	testNS, testContext, testBT, testImpersonate := "test-namespace", "test-context", "test-bearer-token", "test-person"

	// Fake directory for kubeconfig
	fakeDir, err := os.MkdirTemp("", "fake-dir")
	assert.NoError(t, err)
	defer os.RemoveAll(fakeDir)

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
		CurrentContext: "my-fake-context",
		AuthInfos: map[string]*api.AuthInfo{
			"my-fake-user": {
				Token: "", // Empty for testing without authentication
			},
		},
	}

	fakeFile := filepath.Join(fakeDir, ".kubeconfig")

	err = clientcmd.WriteToFile(*config, fakeFile)
	assert.NoError(t, err)

	fakeConfigFlags := &genericclioptions.ConfigFlags{
		Namespace:        &testNS,
		Context:          &testContext,
		KubeConfig:       &fakeFile,
		BearerToken:      &testBT,
		Impersonate:      &testImpersonate,
		ImpersonateGroup: &[]string{"fake-group"},
	}

	tests := []struct {
		name        string
		opts        *CreateClusterOptions
		expectError string
	}{
		{
			name: "Test validation when no context is set",
			opts: &CreateClusterOptions{
				configFlags: genericclioptions.NewConfigFlags(false),
				ioStreams:   &testStreams,
			},
			expectError: "no context is currently set, use \"kubectl config use-context <context>\" to select a new one",
		},
		{
			name: "Successful submit job validation with RayJob",
			opts: &CreateClusterOptions{
				configFlags:    fakeConfigFlags,
				ioStreams:      &testStreams,
				clusterName:    "fakeclustername",
				rayVersion:     "ray-version",
				image:          "ray-image",
				headCPU:        "5",
				headGPU:        "1",
				headMemory:     "5Gi",
				workerReplicas: 3,
				workerCPU:      "4",
				workerMemory:   "5Gi",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectError != "" {
				assert.Error(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
