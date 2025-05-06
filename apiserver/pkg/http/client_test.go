package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	rpcStatus "google.golang.org/genproto/googleapis/rpc/status"

	api "github.com/ray-project/kuberay/proto/go_client"
)

// Mock transport that fail the first two request and succeed the third, which triggers retry
type transportForRetry struct {
	statusErr   *rpcStatus.Status
	succeedBody string
	callCount   int
}

func (r *transportForRetry) RoundTrip(_ *http.Request) (*http.Response, error) {
	r.callCount++

	switch r.callCount {
	case 1:
		return nil, errors.New("mock network error")
	case 2:
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body: io.NopCloser(bytes.NewBufferString(
				fmt.Sprintf(`{"code": %d, "message": "%s"}`, r.statusErr.Code, r.statusErr.Message),
			)),
			Header: make(http.Header),
		}, nil
	case 3:
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(r.succeedBody)),
			Header:     make(http.Header),
		}, nil
	default:
		return nil, fmt.Errorf("unexpected call count: %d", r.callCount)
	}
}

func TestUnmarshalHttpResponseOK(t *testing.T) {
	client := NewKuberayAPIServerClient("baseurl", nil /*httpClient*/, 3 /*maxRetry*/)
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
	client := NewKuberayAPIServerClient("baseurl", nil /*httpClient*/, 3 /* maxRetry */)
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

func TestAPISErverClientRetry(t *testing.T) {
	statusErr := &rpcStatus.Status{Code: 13, Message: "Internal server error"}
	succeedBody := "success"

	// create mock http request
	req, err := http.NewRequest("GET", "http://mock/test", nil /* body */)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	tests := []struct {
		expectErr    error
		expectStatus *rpcStatus.Status
		name         string
		expectBody   []byte
		maxRetry     int
	}{
		{
			name:         "Retries and succeed on third retry",
			maxRetry:     3,
			expectErr:    nil,
			expectStatus: nil,
			expectBody:   []byte(succeedBody),
		},
		{
			name:         "Fails after max retries with internal server error",
			maxRetry:     2,
			expectStatus: statusErr,
			expectErr: &KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusInternalServerError,
			},
			expectBody: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryTransport := &transportForRetry{
				statusErr:   statusErr,
				succeedBody: succeedBody,
			}
			mockClient := &http.Client{Transport: retryTransport}

			client := NewKuberayAPIServerClient("baseurl", mockClient, tt.maxRetry)
			body, status, err := client.executeRequest(req, "http://mock/test")

			if tt.expectErr == nil {
				require.NoError(t, err)
			} else {
				require.Equal(t, tt.expectErr, err)
			}

			if tt.expectStatus == nil {
				require.Empty(t, status)
			} else {
				// only check Code and Message value
				partialStatus := &rpcStatus.Status{
					Code:    status.Code,
					Message: status.Message,
				}
				require.Equal(t, tt.expectStatus, partialStatus)
			}

			if tt.expectBody == nil {
				require.Empty(t, body)
			} else {
				require.Equal(t, tt.expectBody, body)
			}

			require.Equal(t, tt.maxRetry, retryTransport.callCount)
		})
	}
}
