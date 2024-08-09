package cluster

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/rest/fake"
	restfake "k8s.io/client-go/rest/fake"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	"k8s.io/kubectl/pkg/scheme"
)

func TestRayClusterLogComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	fakeClusterLogOptions := NewClusterLogOptions(testStreams)
	fakeArgs := []string{"Expected output"}

	fakeClusterLogOptions.outputDir = "Second Expected Output"

	err := fakeClusterLogOptions.Complete(fakeArgs)
	if err != nil {
		t.Fatalf("Error calling Complete(): %v", err)
	}

	assert.True(t, fakeClusterLogOptions.downloadLog)
	assert.Equal(t, fakeClusterLogOptions.args, fakeArgs)
}

func TestRayClusterLogValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()

	testNS, testContext, testBT, testImpersonate := "test-namespace", "test-contet", "test-bearer-token", "test-person"

	// Fake directory for kubeconfig
	fakeDir, err := os.MkdirTemp("", "fake-config")
	if err != nil {
		t.Fatalf("Error setting up make kubeconfig: %v", err)
	}
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

	if err := clientcmd.WriteToFile(*config, fakeFile); err != nil {
		t.Fatalf("Failed to write kubeconfig to temp file: %v", err)
	}

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
		opts        *ClusterLogOptions
		expect      string
		expectError string
	}{
		{
			name: "Test validation when no context is set",
			opts: &ClusterLogOptions{
				configFlags: genericclioptions.NewConfigFlags(false),
				outputDir:   "",
				args:        []string{"fake-cluster"},
				ioStreams:   &testStreams,
				downloadLog: false,
			},
			expectError: "no context is currently set, use \"kubectl config use-context <context>\" to select a new one",
		},
		{
			name: "Test validation when more than 1 arg",
			opts: &ClusterLogOptions{
				// Use fake config to bypass the config flag checks
				configFlags: fakeConfigFlags,
				outputDir:   "",
				args:        []string{"fake-cluster", "another-fake"},
				ioStreams:   &testStreams,
				downloadLog: false,
			},
			expectError: "must have only one argument",
		},
		{
			name: "Successful validation call",
			opts: &ClusterLogOptions{
				// Use fake config to bypass the config flag checks
				configFlags: fakeConfigFlags,
				outputDir:   "",
				args:        []string{"random_arg"},
				ioStreams:   &testStreams,
				downloadLog: false,
			},
			expectError: "",
		},
		{
			name: "Successfule validation call with output directory",
			opts: &ClusterLogOptions{
				// Use fake config to bypass the config flag checks
				configFlags: fakeConfigFlags,
				outputDir:   fakeDir,
				args:        []string{"random_arg"},
				ioStreams:   &testStreams,
				downloadLog: true,
			},
			expectError: "",
		},
		{
			name: "Failed validation call with output directory not exist",
			opts: &ClusterLogOptions{
				// Use fake config to bypass the config flag checks
				configFlags: fakeConfigFlags,
				outputDir:   "randomPath-here",
				args:        []string{"random_arg"},
				ioStreams:   &testStreams,
				downloadLog: true,
			},
			expectError: "Directory does not exist. Failed with stat randomPath-here: no such file or directory",
		},
		{
			name: "Failed validation call with output directory is file",
			opts: &ClusterLogOptions{
				// Use fake config to bypass the config flag checks
				configFlags: fakeConfigFlags,
				outputDir:   fakeFile,
				args:        []string{"random_arg"},
				ioStreams:   &testStreams,
				downloadLog: true,
			},
			expectError: "Path is Not a directory. Please input a directory and try again",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectError != "" {
				assert.Equal(t, tc.expectError, err.Error())
			} else {
				assert.True(t, err == nil)
			}
		})
	}
}

func TestRayClusterLogRun(t *testing.T) {
	tf := cmdtesting.NewTestFactory().WithNamespace("test")
	defer tf.Cleanup()

	testStreams, _, resBuf, _ := genericclioptions.NewTestIOStreams()

	fakeClusterLogOptions := NewClusterLogOptions(testStreams)
	fakeClusterLogOptions.args = []string{"test-cluster"}

	// Create list of fake ray heads
	rayHeadsList := &v1.PodList{
		ListMeta: metav1.ListMeta{
			ResourceVersion: "15",
		},
		Items: []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-kuberay-head-1",
					Namespace: "test",
					Labels: map[string]string{
						"ray.io/group":    "headgroup",
						"ray.io/clusters": "test-cluster",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "mycontainer",
							Image: "nginx:latest",
						},
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-kuberay-head-2",
					Namespace: "test",
					Labels: map[string]string{
						"ray.io/group":    "headgroup",
						"ray.io/clusters": "test-cluster",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "anothercontainer",
							Image: "busybox:latest",
						},
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodPending,
				},
			},
		},
	}

	fakeLogsHead1 := "This is some fake log data for first pod.\nStill first pod logs\n"
	fakeLogsHead2 := "This is some fake log data for second pod.\nStill second pod logs\n"
	logReader1 := io.NopCloser(bytes.NewReader([]byte(fakeLogsHead1)))
	logReader2 := io.NopCloser(bytes.NewReader([]byte(fakeLogsHead2)))

	codec := scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)
	tf.Client = &restfake.RESTClient{
		GroupVersion:         v1.SchemeGroupVersion,
		NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
		Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/api/v1/pods":
				return &http.Response{StatusCode: http.StatusOK, Header: cmdtesting.DefaultHeader(), Body: cmdtesting.ObjBody(codec, rayHeadsList)}, nil
			case "/api/v1/namespaces/test/pods/test-cluster-kuberay-head-1/log":
				return &http.Response{StatusCode: http.StatusOK, Header: cmdtesting.DefaultHeader(), Body: logReader1}, nil
			case "/api/v1/namespaces/test/pods/test-cluster-kuberay-head-2/log":
				return &http.Response{StatusCode: http.StatusOK, Header: cmdtesting.DefaultHeader(), Body: logReader2}, nil
			default:
				t.Fatalf("request url: %#v,and request: %#v", req.URL, req)
				return nil, nil
			}
		}),
	}
	tf.ClientConfigVal = &restclient.Config{ContentConfig: restclient.ContentConfig{GroupVersion: &v1.SchemeGroupVersion}}

	err := fakeClusterLogOptions.Run(context.Background(), tf)
	if err != nil {
		t.Fatalf("Error calling Run(): %v", err)
	}

	expectedResult := "Head Name: test-cluster-kuberay-head-1\n" + fakeLogsHead1 + "Head Name: test-cluster-kuberay-head-2\n" + fakeLogsHead2
	if e, a := expectedResult, resBuf.String(); e != a {
		t.Errorf("\nexpected\n%v\ngot\n%v", e, a)
	}
}
