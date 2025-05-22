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

func TestMetricRayJobInfo(t *testing.T) {
	tests := []struct {
		name            string
		rayJobs         []rayv1.RayJob
		expectedMetrics []string
	}{
		{
			name: "two jobs and delete one later",
			rayJobs: []rayv1.RayJob{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-job-1",
						Namespace: "ns1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-job-2",
						Namespace: "ns2",
					},
				},
			},
			expectedMetrics: []string{
				`kuberay_job_info{name="ray-job-1",namespace="ns1"} 1`,
				`kuberay_job_info{name="ray-job-2",namespace="ns2"} 1`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k8sScheme := runtime.NewScheme()
			require.NoError(t, rayv1.AddToScheme(k8sScheme))

			objs := make([]client.Object, len(tc.rayJobs))
			for i := range tc.rayJobs {
				objs[i] = &tc.rayJobs[i]
			}
			client := fake.NewClientBuilder().WithScheme(k8sScheme).WithObjects(objs...).Build()
			manager := NewRayJobMetricsManager(context.Background(), client)
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

			if len(tc.rayJobs) > 0 {
				err = client.Delete(t.Context(), &tc.rayJobs[0])
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

func TestMetricRayJobDeploymentStatus(t *testing.T) {
	tests := []struct {
		name            string
		rayJobs         []rayv1.RayJob
		expectedMetrics []string
	}{
		{
			name: "two jobs with different deployment statuses",
			rayJobs: []rayv1.RayJob{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-job-1",
						Namespace: "ns1",
					},
					Status: rayv1.RayJobStatus{
						JobDeploymentStatus: rayv1.JobDeploymentStatusRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-job-2",
						Namespace: "ns2",
					},
					Status: rayv1.RayJobStatus{
						JobDeploymentStatus: rayv1.JobDeploymentStatusFailed,
					},
				},
			},
			expectedMetrics: []string{
				`kuberay_job_deployment_status{deployment_status="Running",name="ray-job-1",namespace="ns1"} 1`,
				`kuberay_job_deployment_status{deployment_status="Failed",name="ray-job-2",namespace="ns2"} 1`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k8sScheme := runtime.NewScheme()
			require.NoError(t, rayv1.AddToScheme(k8sScheme))

			objs := make([]client.Object, len(tc.rayJobs))
			for i := range tc.rayJobs {
				objs[i] = &tc.rayJobs[i]
			}
			client := fake.NewClientBuilder().WithScheme(k8sScheme).WithObjects(objs...).Build()
			manager := NewRayJobMetricsManager(context.Background(), client)
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

			if len(tc.rayJobs) > 0 {
				err = client.Delete(t.Context(), &tc.rayJobs[0])
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
