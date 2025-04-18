package e2e

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/wait"

	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

//go:embed resources/*.py
var files embed.FS

// CreateHttpRequest instantiates a http request for the  specified endpoint and host
func CreateHttpRequest(method string, host string, endPoint string, body io.Reader) (*http.Request, error) {
	url := host + endPoint
	req, err := http.NewRequestWithContext(context.TODO(), method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	return req, nil
}

// MakeBodyReader creates a io.Reader from the supplied string if is not empty after
// trimming the spaces
func MakeBodyReader(s string) io.Reader {
	if strings.TrimSpace(s) != "" {
		return strings.NewReader(s)
	}
	return nil
}

// PrettyPrintResponseBody generates a "pretty" formatted JSON string from the body
func PrettyPrintResponseBody(body io.ReadCloser) (string, error) {
	inputBytez, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, inputBytez, "", "\t"); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

func ReadFileAsString(t *testing.T, fileName string) string {
	file, err := files.ReadFile(fileName)
	require.NoErrorf(t, err, "No error expected when reading embedded file: '%s'", fileName)
	return string(file)
}

// waitForClusterConditions waits for the cluster to be in one of the expected conditions
// if no expected conditions are provided, it skips the wait
func waitForClusterConditions(t *testing.T, tCtx *End2EndTestingContext, clusterName string, expectedConditions []rayv1api.RayClusterConditionType) {
	if len(expectedConditions) == 0 {
		// no expected conditions provided, skip the wait
		return
	}
	// wait for the cluster to be in one of the expected conditions for 3 minutes
	// if it is not in one of those conditions, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayCluster, err := tCtx.GetRayClusterByName(clusterName)
		if err != nil {
			return true, err
		}
		t.Logf("Waiting for ray cluster '%s' to be in one of the expected conditions %s", clusterName, expectedConditions)
		for _, condition := range expectedConditions {
			if meta.IsStatusConditionTrue(rayCluster.Status.Conditions, string(condition)) {
				t.Logf("Found condition '%s' for ray cluster '%s'", string(condition), clusterName)
				return true, nil
			}
		}
		return false, nil
	})
	require.NoErrorf(t, err, "No error expected when getting ray cluster: '%s', err %v", tCtx.GetRayClusterName(), err)
}

func waitForRunningCluster(t *testing.T, tCtx *End2EndTestingContext, clusterName string) {
	waitForClusterConditions(t, tCtx, clusterName, []rayv1api.RayClusterConditionType{rayv1api.RayClusterProvisioned})
}

func waitForDeletedCluster(t *testing.T, tCtx *End2EndTestingContext, clusterName string) {
	// wait for the cluster to be deleted
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		_, err = tCtx.GetRayClusterByName(clusterName)
		if err != nil && strings.Contains(err.Error(), "rayclusters.ray.io \""+tCtx.GetRayClusterName()+"\" not found") {
			return true, nil
		}
		t.Logf("Found ray cluster '%s'", clusterName)
		return false, err
	})
	require.NoErrorf(t, err, "No error expected when deleting ray cluster: '%s', err %v", clusterName, err)
}
