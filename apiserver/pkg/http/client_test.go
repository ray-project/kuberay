package http

import (
	"net/http"
	"strings"
	"testing"

	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/stretchr/testify/require"
	rpcStatus "google.golang.org/genproto/googleapis/rpc/status"
)

func TestUnmarshalHttpResponseOK(t *testing.T) {
	client := NewKuberayAPIServerClient("baseurl", nil /*httpClient*/)
	client.executeHttpRequest = func(httpRequest *http.Request, URL string) ([]byte, *rpcStatus.Status, error) {
		resp := &api.ListClustersResponse{
			Clusters: []*api.Cluster{
				{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
			},
		}
		bytes, err := client.marshaler.Marshal(resp)
		require.NoError(t, err)
		return bytes, nil, nil
	}

	req := &api.ListClustersRequest{}
	resp, status, err := client.ListClusters(req)
	require.NoError(t, err)
	require.Nil(t, status)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Clusters)
	require.Equal(t, 1, len(resp.Clusters))
	require.Equal(t, "test-cluster", resp.Clusters[0].Name)
	require.Equal(t, "test-namespace", resp.Clusters[0].Namespace)
}

// Unmarshal response fails and check error returned.
func TestUnmarshalHttpResponseFails(t *testing.T) {
	client := NewKuberayAPIServerClient("baseurl", nil /*httpClient*/)
	client.executeHttpRequest = func(httpRequest *http.Request, URL string) ([]byte, *rpcStatus.Status, error) {
		// Intentionall returning a bad response.
		return []byte("helloworld"), nil, nil
	}

	req := &api.ListClustersRequest{}
	resp, status, err := client.ListClusters(req)
	require.Nil(t, status)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "failed to unmarshal"), err.Error())
	require.Nil(t, resp)
}

func TestAPIServerClientError(t *testing.T) {
	httpErr := KuberayAPIServerClientError{
		HTTPStatusCode: 500,
	}
	require.Equal(t, httpErr.Error(), "kuberay api server request failed with HTTP status (500: Internal Server Error)")
}
