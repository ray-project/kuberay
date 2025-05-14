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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestRayClusterMetricsManager(t *testing.T) {
	tests := []struct {
		name           string
		clusters       []rayv1.RayCluster
		expectedLabels []string
	}{
		{
			name: "two clusters, one with owner, one without",
			clusters: []rayv1.RayCluster{
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
			},
			expectedLabels: []string{
				`kuberay_cluster_info{name="test-ray-cluster",namespace="default",owner_kind="RayJob"} 1`,
				`kuberay_cluster_info{name="test-ray-cluster-2",namespace="default",owner_kind="None"} 1`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k8sScheme := runtime.NewScheme()
			require.NoError(t, rayv1.AddToScheme(k8sScheme))

			objs := make([]client.Object, len(tc.clusters))
			for i := range tc.clusters {
				objs[i] = &tc.clusters[i]
			}
			client := fake.NewClientBuilder().WithScheme(k8sScheme).WithObjects(objs...).Build()
			manager := NewRayClusterMetricsManager(client)
			reg := prometheus.NewRegistry()
			reg.MustRegister(manager)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
			require.NoError(t, err)
			rr := httptest.NewRecorder()
			handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			for _, label := range tc.expectedLabels {
				assert.Contains(t, body, label)
			}
		})
	}
}
