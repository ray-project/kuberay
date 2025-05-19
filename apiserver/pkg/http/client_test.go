package http

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	rpcStatus "google.golang.org/genproto/googleapis/rpc/status"

	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
)

type mockTransport struct {
	statusErr      *rpcStatus.Status
	body           string
	statusSequence []int
	callCount      int
	returnDoError  bool
}

func (m *mockTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	if m.returnDoError {
		return nil, errors.New("mock go error")
	}

	index := m.callCount
	status := http.StatusOK
	if index < len(m.statusSequence) {
		status = m.statusSequence[index]
	}

	respBody := m.body
	if status != http.StatusOK && m.statusErr != nil {
		statusMsg := fmt.Sprintf(`{"code": %d, "message": "%s"}`, m.statusErr.Code, m.statusErr.Message)
		respBody = statusMsg
	}

	m.callCount++

	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(respBody)),
	}, nil
}

func TestUnmarshalHttpResponseOK(t *testing.T) {
	client := NewKuberayAPIServerClient("baseurl", nil /*httpClient*/, util.HTTPClientDefaultMaxRetry)
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
	client := NewKuberayAPIServerClient("baseurl", nil /*httpClient*/, util.HTTPClientDefaultMaxRetry)
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
	req, err := http.NewRequest("GET", "http://mock/test", nil)
	require.NoError(t, err)

	tests := []struct {
		expectErr    error
		transport    http.RoundTripper
		expectStatus *rpcStatus.Status
		name         string
		expectBody   []byte
		maxRetry     int
	}{
		{
			name:     "Retries and succeeds on third retry",
			maxRetry: 3,
			transport: &mockTransport{
				statusSequence: []int{http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusOK},
				body:           succeedBody,
			},
			expectErr:    nil,
			expectStatus: nil,
			expectBody:   []byte(succeedBody),
		},
		{
			name:     "Fails after max retries with internal server error (retryable)",
			maxRetry: 2,
			transport: &mockTransport{
				statusSequence: []int{http.StatusServiceUnavailable, http.StatusInternalServerError},
				statusErr:      statusErr,
			},
			expectStatus: statusErr,
			expectErr: &KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusInternalServerError,
			},
			expectBody: nil,
		},
		{
			name:     "Stops on first non-retryable error (403 Forbidden)",
			maxRetry: 3,
			transport: &mockTransport{
				statusSequence: []int{http.StatusForbidden},
				statusErr:      &rpcStatus.Status{Code: 7, Message: "Permission Denied"},
			},
			expectStatus: &rpcStatus.Status{Code: 7, Message: "Permission Denied"},
			expectErr: &KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusForbidden,
			},
			expectBody: nil,
		},
		{
			name:     "Stops on Go error",
			maxRetry: 3,
			transport: &mockTransport{
				returnDoError: true,
			},
			expectErr:    errors.New("mock go error"),
			expectStatus: nil,
			expectBody:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &http.Client{Transport: tt.transport}
			client := NewKuberayAPIServerClient("baseurl", mockClient, tt.maxRetry)

			body, status, err := client.executeRequest(req, "http://mock/test")

			if tt.expectErr == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectErr.Error())
			}

			if tt.expectStatus == nil {
				require.Empty(t, status)
			} else {
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
		})
	}
}
