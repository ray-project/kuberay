package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/test/support"
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

			req, rr, handler := support.CreateAndExecuteMetricsRequest(t, reg)

			assert.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			for _, label := range tc.expectedMetrics {
				assert.Contains(t, body, label)
			}

			if len(tc.rayJobs) > 0 {
				err := client.Delete(t.Context(), &tc.rayJobs[0])
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

func TestDeleteRayJobMetrics(t *testing.T) {
	k8sScheme := runtime.NewScheme()
	require.NoError(t, rayv1.AddToScheme(k8sScheme))
	client := fake.NewClientBuilder().WithScheme(k8sScheme).Build()
	manager := NewRayJobMetricsManager(context.Background(), client)
	reg := prometheus.NewRegistry()
	reg.MustRegister(manager)

	// Test case 1: Delete specific job metrics
	// Manually add some metrics
	manager.ObserveRayJobExecutionDuration("job1", "ns1", rayv1.JobDeploymentStatusComplete, 0, 10.5)
	manager.ObserveRayJobExecutionDuration("job2", "ns2", rayv1.JobDeploymentStatusFailed, 1, 20.3)
	manager.ObserveRayJobExecutionDuration("job3", "ns1", rayv1.JobDeploymentStatusRunning, 0, 5.7)

	// Test deleting metrics for job1 in ns1
	manager.DeleteRayJobMetrics("job1", "ns1")

	// Verify metrics
	req, recorder, handler := support.CreateAndExecuteMetricsRequest(t, reg)

	assert.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.NotContains(t, body, `kuberay_job_execution_duration_seconds{job_deployment_status="Complete",name="job1",namespace="ns1",retry_count="0"}`)
	assert.Contains(t, body, `kuberay_job_execution_duration_seconds{job_deployment_status="Failed",name="job2",namespace="ns2",retry_count="1"}`)
	assert.Contains(t, body, `kuberay_job_execution_duration_seconds{job_deployment_status="Running",name="job3",namespace="ns1",retry_count="0"}`)

	// Test case 2: Delete with empty name
	manager.DeleteRayJobMetrics("", "ns1")

	// Verify metrics again
	recorder2 := httptest.NewRecorder()
	handler.ServeHTTP(recorder2, req)

	assert.Equal(t, http.StatusOK, recorder2.Code)
	body2 := recorder2.Body.String()
	assert.NotContains(t, body2, `kuberay_job_execution_duration_seconds{job_deployment_status="Complete",name="job1",namespace="ns1",retry_count="0"}`)
	assert.Contains(t, body2, `kuberay_job_execution_duration_seconds{job_deployment_status="Failed",name="job2",namespace="ns2",retry_count="1"}`)
	assert.Contains(t, body2, `kuberay_job_execution_duration_seconds{job_deployment_status="Running",name="job3",namespace="ns1",retry_count="0"}`)

	// Test case 3: Delete with empty name and namespace
	manager.DeleteRayJobMetrics("", "")

	// Verify no metrics were deleted
	recorder3 := httptest.NewRecorder()
	handler.ServeHTTP(recorder3, req)

	assert.Equal(t, http.StatusOK, recorder3.Code)
	body3 := recorder3.Body.String()
	assert.NotContains(t, body3, `kuberay_job_execution_duration_seconds{job_deployment_status="Complete",name="job1",namespace="ns1",retry_count="0"}`)
	assert.Contains(t, body3, `kuberay_job_execution_duration_seconds{job_deployment_status="Failed",name="job2",namespace="ns2",retry_count="1"}`)
	assert.Contains(t, body3, `kuberay_job_execution_duration_seconds{job_deployment_status="Running",name="job3",namespace="ns1",retry_count="0"}`)

	// Test case 4: Delete with false name and namespace
	manager.DeleteRayJobMetrics("ns2", "job2")

	// Verify no metrics were deleted
	recorder4 := httptest.NewRecorder()
	handler.ServeHTTP(recorder4, req)

	assert.Equal(t, http.StatusOK, recorder4.Code)
	body4 := recorder4.Body.String()
	assert.NotContains(t, body4, `kuberay_job_execution_duration_seconds{job_deployment_status="Complete",name="job1",namespace="ns1",retry_count="0"}`)
	assert.Contains(t, body4, `kuberay_job_execution_duration_seconds{job_deployment_status="Failed",name="job2",namespace="ns2",retry_count="1"}`)
	assert.Contains(t, body4, `kuberay_job_execution_duration_seconds{job_deployment_status="Running",name="job3",namespace="ns1",retry_count="0"}`)

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

			req, rr, handler := support.CreateAndExecuteMetricsRequest(t, reg)

			assert.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			for _, label := range tc.expectedMetrics {
				assert.Contains(t, body, label)
			}

			if len(tc.rayJobs) > 0 {
				err := client.Delete(context.Background(), &tc.rayJobs[0])
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
