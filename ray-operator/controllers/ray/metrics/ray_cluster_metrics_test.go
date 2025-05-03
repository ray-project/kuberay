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
	"k8s.io/apimachinery/pkg/labels"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayclusterlister "github.com/ray-project/kuberay/ray-operator/pkg/client/listers/ray/v1"
)

type fakeRayClusterLister struct {
	clusters []*rayv1.RayCluster
}

func (f *fakeRayClusterLister) List(_ labels.Selector) ([]*rayv1.RayCluster, error) {
	return f.clusters, nil
}

func (f *fakeRayClusterLister) RayClusters(_ string) rayclusterlister.RayClusterNamespaceLister {
	return nil
}

func newFakeRayClusters() []*rayv1.RayCluster {
	return []*rayv1.RayCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ray-cluster",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "RayJob"},
				},
			},
			Status: rayv1.RayClusterStatus{
				State: "Running",
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
				Name:            "test-ray-cluster-2",
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{},
			},
			Status: rayv1.RayClusterStatus{
				State: "Running",
				Conditions: []metav1.Condition{
					{
						Type:   string(rayv1.RayClusterProvisioned),
						Status: metav1.ConditionTrue,
					},
					{
						Type:   string(rayv1.HeadPodReady),
						Status: metav1.ConditionTrue,
					},
				},
			},
		},
	}
}

func TestRayClusterMetricsManager(t *testing.T) {
	clusters := newFakeRayClusters()
	lister := &fakeRayClusterLister{clusters: clusters}
	manager := NewRayClusterMetricsManager(lister)

	reg := prometheus.NewRegistry()
	reg.MustRegister(manager)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `kuberay_cluster_info{condition="RayClusterProvisioned",name="test-ray-cluster",namespace="default",owner_kind="RayJob",status="Running"} 1`)
	assert.Contains(t, rr.Body.String(), `kuberay_cluster_info{condition="RayClusterProvisioned",name="test-ray-cluster-2",namespace="default",owner_kind="none",status="Running"} 1`)
}
