package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ray-project/kuberay/release-test/pkg/util"
)

func TestBlobSHA(t *testing.T) {
	// The value `git hash-object` prints for the same content.
	assert.Equal(t, "ce013625030ba8dba906f756967f9e9ca394464a", BlobSHA([]byte("hello\n")))
}

func TestDocsPagePath(t *testing.T) {
	_, ok := docsPagePath(util.DocsMaster+"cluster/kubernetes/examples/ml-example.html", util.DocsMaster+"kuberay/index.html")
	assert.False(t, ok, "a removed page redirects to the section index")

	page, ok := docsPagePath(util.DocsMaster+"kuberay/examples/index.html", util.DocsMaster+"kuberay/examples/index.html")
	assert.Equal(t, "kuberay/examples/index", page)
	assert.True(t, ok, "an index page asked for by name")
}

func TestPageCommands(t *testing.T) {
	// commands is what a page with prose and one code block, cmd, is hashed by.
	commands := func(prose, cmd string) string {
		return string(pageCommands([]byte("# Title\n\n" + prose + "\n\n```sh\n" + cmd + "\n```\n\nMore prose.\n")))
	}
	same := func(before, after string, msg string) {
		t.Helper()
		assert.Equal(t, commands("x", before), commands("x", after), msg)
	}
	differ := func(before, after string, msg string) {
		t.Helper()
		assert.NotEqual(t, commands("x", before), commands("x", after), msg)
	}

	assert.Equal(t, commands("Install it.", "kubectl get pods"), commands("Install it, then wait.", "kubectl get pods"), "prose")
	differ("kubectl get pods", "kubectl get pods -A", "a command")
	same("# List the Ray Pods\nkubectl get pods", "# List the RayCluster's Pods\nkubectl get pods", "a comment")
	same("helm install kuberay-operator kuberay/kuberay-operator \\\n  --version 1.7.0 \\\n  --set a=b",
		"helm install kuberay-operator kuberay/kuberay-operator \\\n  --version 1.8.0 \\\n  --set a=b", "the chart version on a continued line")
	differ("helm install prometheus prometheus-community/kube-prometheus-stack --version 48.2.1",
		"helm install prometheus prometheus-community/kube-prometheus-stack --version 49.0.0", "a third-party chart version")
	same("kubectl apply -f https://raw.githubusercontent.com/ray-project/kuberay/v1.7.0/ray-operator/config/samples/ray-cluster.sample.yaml",
		"kubectl apply -f https://raw.githubusercontent.com/ray-project/kuberay/v1.8.0-rc.0/ray-operator/config/samples/ray-cluster.sample.yaml", "a raw URL")
	same("curl -LO https://github.com/ray-project/kuberay/releases/download/v1.7.0/kubectl-ray_v1.7.0_linux_amd64.tar.gz",
		"curl -LO https://github.com/ray-project/kuberay/releases/download/v1.8.0/kubectl-ray_v1.8.0_linux_amd64.tar.gz", "the plugin tarball")
	same("image: quay.io/kuberay/operator:v1.7.0", "image: quay.io/kuberay/operator:v1.8.0", "the operator image")
	same("    targetRevision: \"1.7.0\"", "    targetRevision: \"1.8.0\"", "ArgoCD targetRevision")
	differ("image: rayproject/ray:2.46.0", "image: rayproject/ray:2.52.0", "a Ray image")

	nested := "`````{tab-set}\n\n````{tab-item} Helm\nSome prose in the tab.\n\n```sh\nhelm install x\n```\n````\n\n`````\n"
	assert.Equal(t, "helm install x", string(pageCommands([]byte(nested))), "only the code inside a directive")
	assert.Equal(t, "helm install x", string(pageCommands([]byte("```{code-block} bash\nhelm install x\n```\n"))), "a code directive")
	prose := "# Concepts\n\nNo commands here.\n"
	assert.Equal(t, prose, string(pageCommands([]byte(prose))), "a page without code blocks is hashed whole")
}

func TestFetchSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v9.9.9/ray-operator/config/samples/x.yaml":
			fmt.Fprint(w, "kind: RayCluster\n")
		case "/v9.9.9/ray-operator/config/samples/limited.yaml":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	old := kuberayRaw
	kuberayRaw = srv.URL + "/"
	defer func() { kuberayRaw = old }()
	root := t.TempDir()
	repoPaths := util.Paths{Root: root, Cache: filepath.Join(root, "release-test", ".cache")}
	ctx := context.Background()
	fetchSample := func(id, name string) (Source, error) {
		return Fetch(ctx, repoPaths, util.Item{ID: id, Source: "ray-operator/config/samples/" + name}, "v9.9.9")
	}

	src, err := fetchSample("x-sample", "x.yaml")
	require.NoError(t, err)
	assert.Equal(t, BlobSHA([]byte("kind: RayCluster\n")), src.SHA, "the sample as it is at the tag, not in this checkout")

	src, err = fetchSample("new-sample", "new.yaml")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/v9.9.9/ray-operator/config/samples/new.yaml", src.Dead, "a sample the tag does not have")

	_, err = fetchSample("limited-sample", "limited.yaml")
	require.ErrorContains(t, err, "HTTP 429", "a rate limit must not be taken for a removed source")
}
