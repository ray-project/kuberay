package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func newFakeRayClusters() []rayv1.RayCluster {
	return []rayv1.RayCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ray-cluster",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "RayJob"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "test-ray-cluster-2",
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{},
			},
		},
	}
}

func TestRayClusterMetricsManager(t *testing.T) {
	clusters := newFakeRayClusters()
	k8sScheme := runtime.NewScheme()
	require.NoError(t, rayv1.AddToScheme(k8sScheme))

	client := fake.NewClientBuilder().WithScheme(k8sScheme).WithObjects(&clusters[0], &clusters[1]).Build()
	manager := NewRayClusterMetricsManager(client)
	reg := prometheus.NewRegistry()
	reg.MustRegister(manager)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `kuberay_cluster_info{name="test-ray-cluster",namespace="default",owner_kind="RayJob"} 1`)
	assert.Contains(t, rr.Body.String(), `kuberay_cluster_info{name="test-ray-cluster-2",namespace="default",owner_kind="none"} 1`)
}
