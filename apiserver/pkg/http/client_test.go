package http

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIServerClientError(t *testing.T) {
	httpErr := KuberayAPIServerClientError{
		HTTPStatusCode: 500,
	}
	require.Equal(t, httpErr.Error(), "kuberay api server request failed with HTTP status (500: Internal Server Error)")
}
