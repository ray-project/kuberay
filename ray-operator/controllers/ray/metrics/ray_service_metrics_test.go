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

func TestRayServiceInfo(t *testing.T) {
	testCases := []struct {
		name         string
		rayServices  []rayv1.RayService
		expectedInfo []string
	}{
		{
			name: "Test RayService info showing correctly",
			rayServices: []rayv1.RayService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-service-1",
						Namespace: "default",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-service-2",
						Namespace: "default",
					},
				},
			},
			expectedInfo: []string{
				`kuberay_service_info{name="ray-service-1",namespace="default"} 1`,
				`kuberay_service_info{name="ray-service-2",namespace="default"} 1`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k8sScheme := runtime.NewScheme()
			require.NoError(t, rayv1.AddToScheme(k8sScheme))
			services := make([]client.Object, len(tc.rayServices))
			for i := range tc.rayServices {
				services[i] = &tc.rayServices[i]
			}
			client := fake.NewClientBuilder().WithScheme(k8sScheme).WithObjects(services...).Build()
			manager := NewRayServiceMetricsManager(context.Background(), client)
			reg := prometheus.NewRegistry()
			reg.MustRegister(manager)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
			require.NoError(t, err)
			rr := httptest.NewRecorder()
			handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			for _, info := range tc.expectedInfo {
				assert.Contains(t, body, info)
			}

			if len(tc.rayServices) > 0 {
				err := client.Delete(t.Context(), &tc.rayServices[0])
				require.NoError(t, err)
			}

			rr2 := httptest.NewRecorder()
			handler.ServeHTTP(rr2, req)

			assert.Equal(t, http.StatusOK, rr2.Code)
			body2 := rr2.Body.String()

			assert.NotContains(t, body2, tc.expectedInfo[0])
			for _, info := range tc.expectedInfo[1:] {
				assert.Contains(t, body2, info)
			}
		})
	}
}

func TestRayServiceCondition(t *testing.T) {
	testCases := []struct {
		name         string
		rayServices  []rayv1.RayService
		expectedInfo []string
	}{
		{
			name: "Test RayService ready showing correctly",
			rayServices: []rayv1.RayService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-service-1",
						Namespace: "default",
					},
					Status: rayv1.RayServiceStatuses{
						Conditions: []metav1.Condition{
							{
								Type:   string(rayv1.RayServiceReady),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-service-2",
						Namespace: "default",
					},
					Status: rayv1.RayServiceStatuses{
						Conditions: []metav1.Condition{
							{
								Type:   string(rayv1.RayServiceReady),
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			expectedInfo: []string{
				`kuberay_service_condition_ready{condition="true",name="ray-service-1",namespace="default"} 1`,
				`kuberay_service_condition_ready{condition="false",name="ray-service-2",namespace="default"} 1`,
			},
		},
		{
			name: "Test RayService upgrade in progress showing correctly",
			rayServices: []rayv1.RayService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-service-1",
						Namespace: "default",
					},
					Status: rayv1.RayServiceStatuses{
						Conditions: []metav1.Condition{
							{
								Type:   string(rayv1.UpgradeInProgress),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ray-service-2",
						Namespace: "default",
					},
					Status: rayv1.RayServiceStatuses{
						Conditions: []metav1.Condition{
							{
								Type:   string(rayv1.UpgradeInProgress),
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			expectedInfo: []string{
				`kuberay_service_condition_upgrade_in_progress{condition="true",name="ray-service-1",namespace="default"} 1`,
				`kuberay_service_condition_upgrade_in_progress{condition="false",name="ray-service-2",namespace="default"} 1`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k8sScheme := runtime.NewScheme()
			require.NoError(t, rayv1.AddToScheme(k8sScheme))
			services := make([]client.Object, len(tc.rayServices))
			for i := range tc.rayServices {
				services[i] = &tc.rayServices[i]
			}
			client := fake.NewClientBuilder().WithScheme(k8sScheme).WithObjects(services...).Build()
			manager := NewRayServiceMetricsManager(context.Background(), client)
			reg := prometheus.NewRegistry()
			reg.MustRegister(manager)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
			require.NoError(t, err)
			rr := httptest.NewRecorder()
			handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			for _, info := range tc.expectedInfo {
				assert.Contains(t, body, info)
			}

			if len(tc.rayServices) > 0 {
				err := client.Delete(t.Context(), &tc.rayServices[0])
				require.NoError(t, err)
			}

			rr2 := httptest.NewRecorder()
			handler.ServeHTTP(rr2, req)

			assert.Equal(t, http.StatusOK, rr2.Code)
			body2 := rr2.Body.String()

			assert.NotContains(t, body2, tc.expectedInfo[0])
			for _, info := range tc.expectedInfo[1:] {
				assert.Contains(t, body2, info)
			}
		})
	}
}
