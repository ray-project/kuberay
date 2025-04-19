package http

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	rpcStatus "google.golang.org/genproto/googleapis/rpc/status"

	api "github.com/ray-project/kuberay/proto/go_client"
)

func TestUnmarshalHttpResponseOK(t *testing.T) {
	client := NewKuberayAPIServerClient("baseurl", nil /*httpClient*/)
	client.executeHttpRequest = func(_ *http.Request, _ string) ([]byte, *rpcStatus.Status, error) {
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
	require.Len(t, resp.Clusters, 1)
	require.Equal(t, "test-cluster", resp.Clusters[0].Name)
	require.Equal(t, "test-namespace", resp.Clusters[0].Namespace)
}

// Unmarshal response fails and check error returned.
func TestUnmarshalHttpResponseFails(t *testing.T) {
	client := NewKuberayAPIServerClient("baseurl", nil /*httpClient*/)
	client.executeHttpRequest = func(_ *http.Request, _ string) ([]byte, *rpcStatus.Status, error) {
		// Intentionall returning a bad response.
		return []byte("helloworld"), nil, nil
	}

	req := &api.ListClustersRequest{}
	resp, status, err := client.ListClusters(req)
	require.Nil(t, status)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to unmarshal", err.Error())
	require.Nil(t, resp)
}

func TestAPIServerClientError(t *testing.T) {
	httpErr := KuberayAPIServerClientError{
		HTTPStatusCode: 500,
	}
	require.Equal(t, "kuberay api server request failed with HTTP status (500: Internal Server Error)", httpErr.Error())
}
