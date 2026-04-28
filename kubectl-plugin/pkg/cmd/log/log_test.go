package log

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/resource"
	k8s_fake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/rest/fake"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/remotecommand"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
)

// Mocked NewSPDYExecutor
var fakeNewSPDYExecutor = func(method string, url *url.URL, inputbuf *bytes.Buffer) (remotecommand.Executor, error) {
	return &fakeExecutor{method: method, url: url, buf: inputbuf}, nil
}

type fakeExecutor struct {
	url    *url.URL
	buf    *bytes.Buffer
	method string
}

// Stream is needed for implementing remotecommand.Execute
func (f *fakeExecutor) Stream(_ remotecommand.StreamOptions) error {
	return nil
}

// downloadRayLogFiles uses StreamWithContext so this is the real function that we are mocking
func (f *fakeExecutor) StreamWithContext(_ context.Context, options remotecommand.StreamOptions) error {
	_, err := io.Copy(options.Stdout, f.buf)
	return err
}

// createFakeTarFile creates the fake tar file that will be used for testing
func createFakeTarFile() (*bytes.Buffer, error) {
	// Create a buffer to hold the tar archive
	tarbuff := new(bytes.Buffer)

	// Create a tar writer
	tw := tar.NewWriter(tarbuff)

	// Define the files/directories to include
	files := []struct {
		ModTime time.Time
		Name    string
		Body    string
		IsDir   bool
		Mode    int64
	}{
		{time.Now(), "/", "", true, 0o755},
		{time.Now(), "file1.txt", "This is the content of file1.txt\n", false, 0o644},
		{time.Now(), "file2.txt", "Content of file2.txt inside subdir\n", false, 0o644},
	}

	// Add each file/directory to the tar archive
	for _, file := range files {
		hdr := &tar.Header{
			Name:    file.Name,
			Mode:    file.Mode,
			ModTime: file.ModTime,
			Size:    int64(len(file.Body)),
		}
		if file.IsDir {
			hdr.Typeflag = tar.TypeDir
		} else {
			hdr.Typeflag = tar.TypeReg
		}

		// Write the header
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}

		// Write the file content (if not a directory)
		if !file.IsDir {
			if _, err := tw.Write([]byte(file.Body)); err != nil {
				return nil, err
			}
		}
	}

	// Close the tar writer
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return tarbuff, nil
}

type FakeRemoteExecutor struct{}

func (dre *FakeRemoteExecutor) CreateExecutor(_ *rest.Config, url *url.URL) (remotecommand.Executor, error) {
	return fakeNewSPDYExecutor("GET", url, new(bytes.Buffer))
}

func TestRayClusterLogComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))
	cmd := &cobra.Command{Use: "log"}
	cmd.Flags().StringP("namespace", "n", "", "")

	tests := []struct {
		name                 string
		nodeType             nodeTypeEnum
		expectedResourceType util.ResourceType
		expectedResourceName string
		args                 []string
		hasErr               bool
	}{
		{
			name:                 "valid request with RayCluster",
			expectedResourceType: util.RayCluster,
			expectedResourceName: "test-raycluster",
			args:                 []string{"test-raycluster"},
			hasErr:               false,
		},
		{
			name:                 "valid request with RayCluster",
			expectedResourceType: util.RayCluster,
			expectedResourceName: "test-raycluster",
			args:                 []string{"rayCluster/test-raycluster"},
			hasErr:               false,
		},
		{
			name:                 "valid request with RayService",
			expectedResourceType: util.RayService,
			expectedResourceName: "test-rayservice",
			args:                 []string{"rayservice/test-rayservice"},
			hasErr:               false,
		},
		{
			name:                 "valid request with RayJob",
			expectedResourceType: util.RayJob,
			expectedResourceName: "test-rayJob",
			args:                 []string{"rayJob/test-rayJob"},
			hasErr:               false,
		},
		{
			name:   "invalid args (no resource type)",
			args:   []string{"/test-resource"},
			hasErr: true,
		},
		{
			name:   "invalid args (no resource name)",
			args:   []string{"raycluster/"},
			hasErr: true,
		},
		{
			name:   "invalid args (invalid resource type)",
			args:   []string{"invalid-type/test-resource"},
			hasErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeClusterLogOptions := NewClusterLogOptions(cmdFactory, testStreams)
			fakeClusterLogOptions.nodeType = tc.nodeType
			err := fakeClusterLogOptions.Complete(cmd, tc.args)
			if tc.hasErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedResourceType, fakeClusterLogOptions.ResourceType)
				assert.Equal(t, tc.expectedResourceName, fakeClusterLogOptions.ResourceName)
			}
		})
	}
}

func TestRayClusterLogValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	tempDir := t.TempDir()
	tempFile, err := os.CreateTemp(tempDir, "temp-file")
	require.NoError(t, err)

	tests := []struct {
		name        string
		opts        *ClusterLogOptions
		expect      string
		expectError string
	}{
		{
			name: "Test validation when node type is `random-string`",
			opts: &ClusterLogOptions{
				cmdFactory:   cmdFactory,
				outputDir:    tempDir,
				ResourceName: "fake-cluster",
				nodeType:     "random-string",
				ioStreams:    &testStreams,
			},
			expectError: "unknown node type `random-string`",
		},
		{
			name: "Successful validation call",
			opts: &ClusterLogOptions{
				cmdFactory:   cmdFactory,
				outputDir:    tempDir,
				ResourceName: "fake-cluster",
				nodeType:     headNodeType,
				ioStreams:    &testStreams,
			},
			expectError: "",
		},
		{
			name: "Validate output directory when no out-dir is set.",
			opts: &ClusterLogOptions{
				cmdFactory:   cmdFactory,
				outputDir:    "",
				ResourceName: "fake-cluster",
				nodeType:     headNodeType,
				ioStreams:    &testStreams,
			},
			expectError: "",
		},
		{
			name: "Failed validation call with output directory not exist",
			opts: &ClusterLogOptions{
				cmdFactory:   cmdFactory,
				outputDir:    "randomPath-here",
				ResourceName: "fake-cluster",
				nodeType:     headNodeType,
				ioStreams:    &testStreams,
			},
			expectError: "Directory does not exist. Failed with: stat randomPath-here: no such file or directory",
		},
		{
			name: "Failed validation call with output directory is file",
			opts: &ClusterLogOptions{
				cmdFactory:   cmdFactory,
				outputDir:    tempFile.Name(),
				ResourceName: "fake-cluster",
				nodeType:     headNodeType,
				ioStreams:    &testStreams,
			},
			expectError: "Path is not a directory. Please input a directory and try again",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				if tc.opts.outputDir == "" {
					assert.Equal(t, tc.opts.ResourceName, tc.opts.outputDir)
				}
				assert.NoError(t, err)
			}
		})
	}
}

func TestRayClusterLogRun(t *testing.T) {
	tf := cmdtesting.NewTestFactory().WithNamespace("test")
	defer tf.Cleanup()

	fakeDir, err := os.MkdirTemp("", "fake-directory")
	require.NoError(t, err)
	defer os.RemoveAll(fakeDir)

	testStreams, _, _, _ := genericiooptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	fakeClusterLogOptions := NewClusterLogOptions(cmdFactory, testStreams)
	// Uses the mocked executor
	fakeClusterLogOptions.Executor = &FakeRemoteExecutor{}
	fakeClusterLogOptions.ResourceName = "test-cluster"
	fakeClusterLogOptions.outputDir = fakeDir
	fakeClusterLogOptions.ResourceType = util.RayCluster
	fakeClusterLogOptions.nodeType = "all"

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

	// create logs for multiple head pods and turn them into io streams so they can be returned with the fake client
	fakeLogs := []string{
		"This is some fake log data for first pod.\nStill first pod logs\n",
		"This is some fake log data for second pod.\nStill second pod logs\n",
	}
	logReader1 := io.NopCloser(bytes.NewReader([]byte(fakeLogs[0])))
	logReader2 := io.NopCloser(bytes.NewReader([]byte(fakeLogs[1])))

	// fakes the client and the REST calls.
	codec := scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)
	tf.Client = &fake.RESTClient{
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

	tf.ClientConfigVal = &rest.Config{
		ContentConfig: rest.ContentConfig{GroupVersion: &v1.SchemeGroupVersion},
	}

	err = fakeClusterLogOptions.Run(context.Background(), tf)
	require.NoError(t, err)

	// Check that the two directories are there
	entries, err := os.ReadDir(fakeDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	assert.Equal(t, "test-cluster-kuberay-head-1", entries[0].Name())
	assert.Equal(t, "test-cluster-kuberay-head-2", entries[1].Name())

	// Check the first directory for the logs
	for ind, entry := range entries {
		currPath := filepath.Join(fakeDir, entry.Name())
		currDir, err := os.ReadDir(currPath)
		require.NoError(t, err)
		assert.Len(t, currDir, 1)
		openfile, err := os.Open(filepath.Join(currPath, "stdout.log"))
		require.NoError(t, err)
		actualContent, err := io.ReadAll(openfile)
		require.NoError(t, err)
		assert.Equal(t, fakeLogs[ind], string(actualContent))
	}
}

func TestDownloadRayLogFiles(t *testing.T) {
	fakeDir, err := os.MkdirTemp("", "fake-directory")
	require.NoError(t, err)
	defer os.RemoveAll(fakeDir)

	testStreams, _, _, _ := genericiooptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	fakeClusterLogOptions := NewClusterLogOptions(cmdFactory, testStreams)
	fakeClusterLogOptions.ResourceName = "test-cluster"
	fakeClusterLogOptions.outputDir = fakeDir

	// create fake tar files to test
	fakeTar, err := createFakeTarFile()
	require.NoError(t, err)

	// Ray head needed for calling the downloadRayLogFiles command
	rayHead := v1.Pod{
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
	}

	executor, _ := fakeNewSPDYExecutor("GET", &url.URL{}, fakeTar)

	err = fakeClusterLogOptions.downloadRayLogFiles(context.Background(), executor, rayHead)
	require.NoError(t, err)

	entries, err := os.ReadDir(fakeDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	// Assert the files
	assert.True(t, entries[0].IsDir())
	files, err := os.ReadDir(filepath.Join(fakeDir, entries[0].Name()))
	require.NoError(t, err)
	assert.Len(t, files, 2)

	expectedfileoutput := []struct {
		Name string
		Body string
	}{
		{"file1.txt", "This is the content of file1.txt\n"},
		{"file2.txt", "Content of file2.txt inside subdir\n"},
	}

	// Goes through and check the temp directory with the downloaded files
	for ind, file := range files {
		fileInfo, err := file.Info()
		require.NoError(t, err)
		curr := expectedfileoutput[ind]

		assert.Equal(t, curr.Name, fileInfo.Name())
		openfile, err := os.Open(filepath.Join(fakeDir, entries[0].Name(), file.Name()))
		require.NoError(t, err)
		actualContent, err := io.ReadAll(openfile)
		require.NoError(t, err)
		assert.Equal(t, curr.Body, string(actualContent))
	}
}

// createFakeTarFileWithInvalidMode creates a tar archive that contains one regular file
// with a header.Mode value outside the valid [0, math.MaxUint32] range (negative).
// This exercises the G115 overflow-guard path added in the fix.
func createFakeTarFileWithInvalidMode() (*bytes.Buffer, error) {
	tarbuff := new(bytes.Buffer)
	tw := tar.NewWriter(tarbuff)

	// A valid directory entry so the archive is well-formed.
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     "/",
		Mode:     0o755,
	}); err != nil {
		return nil, err
	}

	// A regular file whose Mode is negative – this triggers the G115 overflow guard.
	body := []byte("should be skipped")
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "bad-mode-file.txt",
		Mode:     -1, // invalid: negative value overflows uint32
		Size:     int64(len(body)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(body); err != nil {
		return nil, err
	}

	// A normal file that should still be extracted after the bad one is skipped.
	body2 := []byte("hello from valid file\n")
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "valid-file.txt",
		Mode:     0o644,
		Size:     int64(len(body2)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(body2); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return tarbuff, nil
}

// createFakeTarFileWithManyFiles creates a tar archive with n regular files.
// Used to verify that the FD-leak fix keeps all file descriptors closed during
// the loop (i.e. no "too many open files" error).
func createFakeTarFileWithManyFiles(n int) (*bytes.Buffer, error) {
	tarbuff := new(bytes.Buffer)
	tw := tar.NewWriter(tarbuff)

	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     "/",
		Mode:     0o755,
	}); err != nil {
		return nil, err
	}

	for i := range n {
		body := []byte(fmt.Sprintf("content of file %d\n", i))
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     fmt.Sprintf("file%04d.txt", i),
			Mode:     0o644,
			Size:     int64(len(body)),
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(body); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return tarbuff, nil
}

// TestDownloadRayLogFiles_SkipsFileWithInvalidMode is a regression test for the
// G115 integer-overflow fix.  A tar entry whose header.Mode is outside the
// valid [0, math.MaxUint32] range must be skipped with a warning; the
// subsequent valid entry must still be extracted normally.
func TestDownloadRayLogFiles_SkipsFileWithInvalidMode(t *testing.T) {
	fakeDir := t.TempDir()

	testStreams, _, out, _ := genericiooptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	opts := NewClusterLogOptions(cmdFactory, testStreams)
	opts.ResourceName = "test-cluster"
	opts.outputDir = fakeDir

	fakeTar, err := createFakeTarFileWithInvalidMode()
	require.NoError(t, err)

	rayHead := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-kuberay-head-1",
			Namespace: "test",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "mycontainer", Image: "nginx:latest"}},
		},
	}

	executor, _ := fakeNewSPDYExecutor("GET", &url.URL{}, fakeTar)
	err = opts.downloadRayLogFiles(context.Background(), executor, rayHead)
	require.NoError(t, err, "downloadRayLogFiles must not return an error for an out-of-range mode")

	// The warning message must have been printed.
	assert.Contains(t, out.String(), "skipping file",
		"a warning must be printed for the file with the invalid mode")

	// The bad file must NOT have been created.
	badPath := filepath.Join(fakeDir, rayHead.Name, "bad-mode-file.txt")
	_, statErr := os.Stat(badPath)
	assert.True(t, os.IsNotExist(statErr),
		"bad-mode-file.txt must not be created when mode is out of range")

	// The valid file that follows the bad one MUST have been created.
	validPath := filepath.Join(fakeDir, rayHead.Name, "valid-file.txt")
	content, readErr := os.ReadFile(validPath)
	require.NoError(t, readErr, "valid-file.txt must be created even after a skipped entry")
	assert.Equal(t, "hello from valid file\n", string(content))
}

// TestDownloadRayLogFiles_NoFDLeakWithManyFiles is a regression test for the
// file-descriptor leak fix.  Before the fix, defer outFile.Close() inside the
// loop body deferred all closes until function return; with a large archive
// this exhausted the per-process FD limit and returned "too many open files".
// The test extracts 300 files – well above the typical per-process soft limit
// of 256 on macOS and comfortably above the default 1024 on Linux when the
// test binary itself already holds several descriptors.
func TestDownloadRayLogFiles_NoFDLeakWithManyFiles(t *testing.T) {
	const fileCount = 300

	fakeDir := t.TempDir()
	testStreams, _, _, _ := genericiooptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	opts := NewClusterLogOptions(cmdFactory, testStreams)
	opts.ResourceName = "test-cluster"
	opts.outputDir = fakeDir

	fakeTar, err := createFakeTarFileWithManyFiles(fileCount)
	require.NoError(t, err)

	rayHead := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-kuberay-head-1",
			Namespace: "test",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "mycontainer", Image: "nginx:latest"}},
		},
	}

	executor, _ := fakeNewSPDYExecutor("GET", &url.URL{}, fakeTar)

	// Must not return an error ("too many open files" would surface here).
	err = opts.downloadRayLogFiles(context.Background(), executor, rayHead)
	require.NoError(t, err, "downloadRayLogFiles must not exhaust file descriptors")

	// Verify all files were actually written.
	podDir := filepath.Join(fakeDir, rayHead.Name)
	entries, err := os.ReadDir(podDir)
	require.NoError(t, err)
	assert.Len(t, entries, fileCount,
		"all %d files must be extracted without FD exhaustion", fileCount)
}

// createTempKubeConfigFile creates a temporary kubeconfig file with the given current context.
func createTempKubeConfigFile(t *testing.T, currentContext string) (string, error) {
	tmpDir := t.TempDir()

	// Set up fake config for kubeconfig
	config := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"test-cluster": {
				Server:                "https://fake-kubernetes-cluster.example.com",
				InsecureSkipTLSVerify: true, // For testing purposes
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"gke_my-project_us-central1-a_my-cluster": {
				Cluster: "test-cluster",
			},
			"minikube": {
				Cluster: "test-cluster",
			},
		},
		CurrentContext: currentContext,
	}

	fakeFile := filepath.Join(tmpDir, ".kubeconfig")

	return fakeFile, clientcmd.WriteToFile(*config, fakeFile)
}

func TestGCPLogLink(t *testing.T) {
	tests := []struct {
		name            string
		currentContext  string
		headPods        *v1.PodList
		expectedGCPURL  string
		expectedWarning string
	}{
		{
			name:           "GKE context with fluentbit",
			currentContext: "gke_my-project_us-central1-a_my-cluster",
			headPods: &v1.PodList{
				Items: []v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "head-pod",
							Labels: map[string]string{
								"ray.io/node-type": "head",
								"ray.io/cluster":   "test-cluster",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{Name: "ray-head"},
								{Name: "fluentbit"},
							},
						},
					},
				},
			},
			expectedGCPURL:  "https://console.cloud.google.com/logs/query;query=resource.type%3D%22k8s_container%22%0Aresource.labels.namespace_name%3D%22default%22%0Alabels.%22k8s-pod%2Fray_io%2Fcluster%22%3D%22test-cluster%22%0Alabels.%22k8s-pod%2Fray_io%2Fis-ray-node%22%3D%22yes%22?project=my-project",
			expectedWarning: "",
		},
		{
			name:           "GKE context without fluentbit",
			currentContext: "gke_my-project_us-central1-a_my-cluster",
			headPods: &v1.PodList{
				Items: []v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "head-pod",
							Labels: map[string]string{
								"ray.io/node-type": "head",
								"ray.io/cluster":   "test-cluster",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{Name: "ray-head"},
							},
						},
					},
				},
			},
			expectedGCPURL:  "https://console.cloud.google.com/logs/query;query=resource.type%3D%22k8s_container%22%0Aresource.labels.namespace_name%3D%22default%22%0Alabels.%22k8s-pod%2Fray_io%2Fcluster%22%3D%22test-cluster%22%0Alabels.%22k8s-pod%2Fray_io%2Fis-ray-node%22%3D%22yes%22?project=my-project",
			expectedWarning: "Warning: The head pod does not have a 'fluentbit' container. Logs might not be exported to Google Cloud Logging.",
		},
		{
			name:            "Non-GKE context",
			currentContext:  "minikube",
			headPods:        &v1.PodList{},
			expectedGCPURL:  "https://console.cloud.google.com/logs/query;query=resource.type%3D%22k8s_container%22%0Aresource.labels.namespace_name%3D%22default%22%0Alabels.%22k8s-pod%2Fray_io%2Fcluster%22%3D%22test-cluster%22%0Alabels.%22k8s-pod%2Fray_io%2Fis-ray-node%22%3D%22yes%22",
			expectedWarning: "Warning: The current kubectl context does not appear to be a GKE cluster. The generated link may not work.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testStreams, _, _, errOut := genericclioptions.NewTestIOStreams()

			kubeConfig, err := createTempKubeConfigFile(t, tc.currentContext)
			require.NoError(t, err)

			tf := cmdutil.NewFactory(&genericclioptions.ConfigFlags{
				KubeConfig: &kubeConfig,
			})

			options := NewClusterLogOptions(tf, testStreams)
			options.namespace = "default"
			options.link = "gke"

			fakeKubeClient := k8s_fake.NewSimpleClientset()
			if tc.headPods != nil {
				for _, pod := range tc.headPods.Items {
					_, err := fakeKubeClient.CoreV1().Pods("default").Create(context.TODO(), &pod, metav1.CreateOptions{})
					require.NoError(t, err)
				}
			}

			clientSet := client.NewClientForTesting(fakeKubeClient, nil)

			err = options.gcpLink(context.Background(), clientSet, "test-cluster")
			require.NoError(t, err)

			assert.Contains(t, testStreams.Out.(*bytes.Buffer).String(), tc.expectedGCPURL)
			if tc.expectedWarning != "" {
				assert.Contains(t, errOut.String(), tc.expectedWarning)
			} else {
				assert.Empty(t, errOut.String())
			}
		})
	}
}
