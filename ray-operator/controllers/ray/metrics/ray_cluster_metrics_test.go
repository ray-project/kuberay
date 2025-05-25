package metrics

import (
	"context"
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

func TestRayClusterInfo(t *testing.T) {
	tests := []struct {
		name            string
		clusters        []rayv1.RayCluster
		expectedMetrics []string
	}{
		{
			name: "two clusters, one with owner, one without",
			clusters: []rayv1.RayCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ray-cluster",
						Namespace: "default",
						Labels: map[string]string{
							"ray.io/originated-from-crd": "RayJob",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ray-cluster-2",
						Namespace: "default",
						Labels:    map[string]string{},
					},
				},
			},
			expectedMetrics: []string{
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
			manager := NewRayClusterMetricsManager(context.Background(), client)
			reg := prometheus.NewRegistry()
			reg.MustRegister(manager)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
			require.NoError(t, err)
			rr := httptest.NewRecorder()
			handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			for _, label := range tc.expectedMetrics {
				assert.Contains(t, body, label)
			}

			if len(tc.clusters) > 0 {
				err = client.Delete(t.Context(), &tc.clusters[0])
				require.NoError(t, err)
			}

			rr2 := httptest.NewRecorder()
			handler.ServeHTTP(rr2, req)

			assert.Equal(t, http.StatusOK, rr2.Code)
			body2 := rr2.Body.String()

			assert.NotContains(t, body2, tc.expectedMetrics[0])
			for _, label := range tc.expectedMetrics[1:] {
				assert.Contains(t, body2, label)
			}
		})
	}
}

func TestRayClusterConditionProvisioned(t *testing.T) {
	tests := []struct {
		name            string
		clusters        []rayv1.RayCluster
		expectedMetrics []string
	}{
		{
			name: "clusters with different provisioned status",
			clusters: []rayv1.RayCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "provisioned-cluster",
						Namespace: "default",
					},
					Status: rayv1.RayClusterStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(rayv1.RayClusterProvisioned),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "unprovisioned-cluster",
						Namespace: "default",
					},
					Status: rayv1.RayClusterStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(rayv1.RayClusterProvisioned),
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			expectedMetrics: []string{
				`kuberay_cluster_condition_provisioned{condition="true",name="provisioned-cluster",namespace="default"} 1`,
				`kuberay_cluster_condition_provisioned{condition="false",name="unprovisioned-cluster",namespace="default"} 1`,
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
			manager := NewRayClusterMetricsManager(context.Background(), client)
			reg := prometheus.NewRegistry()
			reg.MustRegister(manager)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
			require.NoError(t, err)
			rr := httptest.NewRecorder()
			handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			for _, metric := range tc.expectedMetrics {
				assert.Contains(t, body, metric)
			}

			if len(tc.clusters) > 0 {
				err = client.Delete(t.Context(), &tc.clusters[0])
				require.NoError(t, err)
			}

			rr2 := httptest.NewRecorder()
			handler.ServeHTTP(rr2, req)

			assert.Equal(t, http.StatusOK, rr2.Code)
			body2 := rr2.Body.String()

			assert.NotContains(t, body2, tc.expectedMetrics[0])
			for _, metric := range tc.expectedMetrics[1:] {
				assert.Contains(t, body2, metric)
			}
		})
	}
}
