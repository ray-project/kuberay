package get

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
)

// This is to test Complete() and ensure that it is setting the namespace and arguments correctly
// No validation test is done here
func TestRayClusterGetComplete(t *testing.T) {
	// Initialize members of the cluster get option struct and the struct itself
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	fakeClusterGetOptions := NewGetClusterOptions(testStreams)
	fakeArgs := []string{"Expected", "output"}

	*fakeClusterGetOptions.configFlags.Namespace = ""
	fakeClusterGetOptions.AllNamespaces = false

	err := fakeClusterGetOptions.Complete(fakeArgs)
	assert.Nil(t, err)

	assert.True(t, fakeClusterGetOptions.AllNamespaces)
	assert.Equal(t, fakeClusterGetOptions.args, fakeArgs)
}

// Test the Validation() step of the command.
func TestRayClusterGetValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()

	testNS, testContext, testBT, testImpersonate := "test-namespace", "test-context", "test-bearer-token", "test-person"

	// Fake directory for kubeconfig
	fakeDir, err := os.MkdirTemp("", "fake-config")
	assert.Nil(t, err)
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
	assert.Nil(t, err)

	// Initialize the fake config flag with the fake kubeconfig and values
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
		opts        *GetClusterOptions
		expect      string
		expectError string
	}{
		{
			name: "Test validation when no context is set",
			opts: &GetClusterOptions{
				configFlags:   genericclioptions.NewConfigFlags(false),
				AllNamespaces: false,
				args:          []string{"random_arg"},
				ioStreams:     &testStreams,
			},
			expectError: "no context is currently set, use \"kubectl config use-context <context>\" to select a new one",
		},
		{
			name: "Test validation when more than 1 arg",
			opts: &GetClusterOptions{
				// Use fake config to bypass the config flag checks
				configFlags:   fakeConfigFlags,
				AllNamespaces: false,
				args:          []string{"fake", "args"},
				ioStreams:     &testStreams,
			},
			expectError: "too many arguments, either one or no arguments are allowed",
		},
		{
			name: "Successful validation call",
			opts: &GetClusterOptions{
				// Use fake config to bypass the config flag checks
				configFlags:   fakeConfigFlags,
				AllNamespaces: false,
				args:          []string{"random_arg"},
				ioStreams:     &testStreams,
			},
			expectError: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectError != "" {
				assert.Equal(t, tc.expectError, err.Error())
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

// Tests the Run() step of the command and ensure that the output is as expected.
func TestRayClusterGetRun(t *testing.T) {
	tf := cmdtesting.NewTestFactory().WithNamespace("test")
	defer tf.Cleanup()

	testStreams, _, resBuf, _ := genericclioptions.NewTestIOStreams()

	fakeClusterGetOptions := NewGetClusterOptions(testStreams)

	// Create fake ray cluster unstructured object for fake rest response
	raycluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "ray.io/v1",
			"kind":       "RayCluster",
			"name":       "raycluster-kuberay",
			"namespace":  "test",
			"metadata": map[string]interface{}{
				"name":      "raycluster-kuberay",
				"namespace": "test",
			},
			"status": map[string]interface{}{
				"desiredWorkerReplicas":   "2",
				"availableWorkerReplicas": "2",
				"desiredCPU":              "6",
				"desiredGPU":              "1",
				"desiredTPU":              "1",
				"desiredMemory":           "24Gi",
				"state":                   "ready",
			},
		},
	}

	// Initialize the printer with an empty print options since we are setting the column definition later
	expectedTestResultTable := printers.NewTablePrinter(printers.PrintOptions{})

	// Define the column names and types
	testResTable := &v1.Table{
		ColumnDefinitions: []v1.TableColumnDefinition{
			{Name: "Name", Type: "string"},
			{Name: "Namespace", Type: "string"},
			{Name: "Desired Workers", Type: "string"},
			{Name: "Available Workers", Type: "string"},
			{Name: "CPUs", Type: "string"},
			{Name: "GPUs", Type: "string"},
			{Name: "TPUs", Type: "string"},
			{Name: "Memory", Type: "string"},
			{Name: "Age", Type: "string"},
		},
	}

	testResTable.Rows = append(testResTable.Rows, v1.TableRow{
		Cells: []interface{}{
			"raycluster-kuberay",
			"test",
			"2",
			"2",
			"6",
			"1",
			"1",
			"24Gi",
			"<unknown>",
		},
	})

	// Result buffer for the expected table result
	var resbuffer bytes.Buffer
	err := expectedTestResultTable.PrintObj(testResTable, &resbuffer)
	assert.Nil(t, err)

	// Create fake dynmaic with the rayscluster
	tf.FakeDynamicClient = fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), raycluster)

	err = fakeClusterGetOptions.Run(context.Background(), tf)
	assert.Nil(t, err)

	if e, a := resbuffer.String(), resBuf.String(); e != a {
		t.Errorf("\nexpected\n%v\ngot\n%v", e, a)
	}
}
