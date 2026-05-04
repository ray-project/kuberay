package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

func Test_decodeConfig(t *testing.T) {
	testcases := []struct {
		name           string
		configData     string
		errContains    string
		expectedConfig configapi.Configuration
		expectErr      bool
	}{
		{
			name: "default config file",
			configData: `apiVersion: config.ray.io/v1alpha1
kind: Configuration
`,
			expectedConfig: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Configuration",
					APIVersion: "config.ray.io/v1alpha1",
				},
				MetricsAddr:          ":8080",
				ProbeAddr:            ":8082",
				EnableLeaderElection: new(true),
				ReconcileConcurrency: 1,
				QPS:                  ptr.To(configapi.DefaultQPS),
				Burst:                ptr.To(configapi.DefaultBurst),
			},
			expectErr: false,
		},
		{
			name: "config file all field set",
			configData: `apiVersion: config.ray.io/v1alpha1
kind: Configuration
metricsAddr: ":8080"
probeAddr: ":8082"
enableLeaderElection: true
reconcileConcurrency: 1
`,
			expectedConfig: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Configuration",
					APIVersion: "config.ray.io/v1alpha1",
				},
				MetricsAddr:          ":8080",
				ProbeAddr:            ":8082",
				EnableLeaderElection: new(true),
				ReconcileConcurrency: 1,
				QPS:                  ptr.To(configapi.DefaultQPS),
				Burst:                ptr.To(configapi.DefaultBurst),
			},
			expectErr: false,
		},
		{
			name: "config with sidecars",
			configData: `apiVersion: config.ray.io/v1alpha1
kind: Configuration
metricsAddr: ":8080"
probeAddr: ":8082"
enableLeaderElection: true
reconcileConcurrency: 1
headSidecarContainers:
- name: fluentbit
  image: fluent/fluent-bit:1.9.6
workerSidecarContainers:
- name: fluentbit
  image: fluent/fluent-bit:1.9.6
`,
			expectedConfig: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Configuration",
					APIVersion: "config.ray.io/v1alpha1",
				},
				MetricsAddr:          ":8080",
				ProbeAddr:            ":8082",
				EnableLeaderElection: new(true),
				ReconcileConcurrency: 1,
				HeadSidecarContainers: []corev1.Container{
					{
						Name:  "fluentbit",
						Image: "fluent/fluent-bit:1.9.6",
					},
				},
				WorkerSidecarContainers: []corev1.Container{
					{
						Name:  "fluentbit",
						Image: "fluent/fluent-bit:1.9.6",
					},
				},
				QPS:   ptr.To(configapi.DefaultQPS),
				Burst: ptr.To(configapi.DefaultBurst),
			},
			expectErr: false,
		},
		{
			name: "unknown filed ignored",
			configData: `apiVersion: config.ray.io/v1alpha1
kind: Configuration
metricsAddr: ":8080"
probeAddr: ":8082"
enableLeaderElection: true
reconcileConcurrency: 1
unknownfield: 1
`,
			expectedConfig: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Configuration",
					APIVersion: "config.ray.io/v1alpha1",
				},
				MetricsAddr:          ":8080",
				ProbeAddr:            ":8082",
				EnableLeaderElection: new(true),
				ReconcileConcurrency: 1,
				QPS:                  ptr.To(configapi.DefaultQPS),
				Burst:                ptr.To(configapi.DefaultBurst),
			},
			expectErr: false,
		},
		{
			name: "set QPS and Burst",
			configData: `apiVersion: config.ray.io/v1alpha1
kind: Configuration
metricsAddr: ":8080"
probeAddr: ":8082"
enableLeaderElection: true
reconcileConcurrency: 1
qps: 150.5
burst: 300
`,
			expectedConfig: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Configuration",
					APIVersion: "config.ray.io/v1alpha1",
				},
				MetricsAddr:          ":8080",
				ProbeAddr:            ":8082",
				EnableLeaderElection: new(true),
				ReconcileConcurrency: 1,
				QPS:                  new(150.5),
				Burst:                new(300),
			},
			expectErr: false,
		},
		{
			name: "set Burst using float",
			configData: `apiVersion: config.ray.io/v1alpha1
kind: Configuration
metricsAddr: ":8080"
probeAddr: ":8082"
enableLeaderElection: true
reconcileConcurrency: 1
qps: 150
burst: 300.5
`,
			expectedConfig: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Configuration",
					APIVersion: "config.ray.io/v1alpha1",
				},
				MetricsAddr:          ":8080",
				ProbeAddr:            ":8082",
				EnableLeaderElection: new(true),
				ReconcileConcurrency: 1,
				QPS:                  new(150.0),
				Burst:                new(300),
			},
			expectErr:   true,
			errContains: "json: cannot unmarshal number 300.5 into Go struct field Configuration.burst of type int",
		},
		{
			name: "invalid type for field",
			configData: `apiVersion: config.ray.io/v1alpha1
kind: Configuration
metricsAddr: ":8080"
probeAddr: ":8082"
enableLeaderElection: true
reconcileConcurrency: true
`,
			expectedConfig: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Configuration",
					APIVersion: "config.ray.io/v1alpha1",
				},
				MetricsAddr:          ":8080",
				ProbeAddr:            ":8082",
				EnableLeaderElection: new(true),
				ReconcileConcurrency: 0,
				QPS:                  ptr.To(configapi.DefaultQPS),
				Burst:                ptr.To(configapi.DefaultBurst),
			},
			expectErr:   true,
			errContains: "json: cannot unmarshal bool into Go struct field Configuration.reconcileConcurrency of type int",
		},
		{
			name: "Set ReconcileConcurrency",
			configData: `apiVersion: config.ray.io/v1alpha1
kind: Configuration
metricsAddr: ":8080"
probeAddr: ":8082"
enableLeaderElection: true
reconcileConcurrency: 100
`,
			expectedConfig: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Configuration",
					APIVersion: "config.ray.io/v1alpha1",
				},
				MetricsAddr:          ":8080",
				ProbeAddr:            ":8082",
				EnableLeaderElection: new(true),
				ReconcileConcurrency: 100,
				QPS:                  ptr.To(configapi.DefaultQPS),
				Burst:                ptr.To(configapi.DefaultBurst),
			},
			expectErr: false,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			config, err := decodeConfig([]byte(testcase.configData), scheme)
			if testcase.expectErr {
				if err == nil {
					t.Error("expected err but got nil")
				}

				if err != nil && !strings.Contains(err.Error(), testcase.errContains) {
					t.Logf("actual error: %v", err)
					t.Logf("expected error to contain string: %q", testcase.errContains)
					t.Error("unexpected error")
				}

				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(config, testcase.expectedConfig) {
				t.Logf("actual config: %v", config)
				t.Logf("expected config: %v", testcase.expectedConfig)
				t.Error("unexpected config")
			}
		})
	}
}

func Test_validateNativeWorkloadSchedulingConfig(t *testing.T) {
	tests := []struct {
		name                 string
		featureGateEnabled   bool
		enableBatchScheduler bool
		batchScheduler       string
		wantErr              bool
		errContains          string
	}{
		// Positive cases: no error expected
		{
			name:                 "all disabled — no conflict",
			featureGateEnabled:   false,
			enableBatchScheduler: false,
			batchScheduler:       "",
			wantErr:              false,
		},
		{
			name:                 "only NativeWorkloadScheduling enabled — no conflict",
			featureGateEnabled:   true,
			enableBatchScheduler: false,
			batchScheduler:       "",
			wantErr:              false,
		},
		{
			name:                 "only EnableBatchScheduler enabled — no conflict",
			featureGateEnabled:   false,
			enableBatchScheduler: true,
			batchScheduler:       "",
			wantErr:              false,
		},
		{
			name:                 "only BatchScheduler set — no conflict",
			featureGateEnabled:   false,
			enableBatchScheduler: false,
			batchScheduler:       "volcano",
			wantErr:              false,
		},
		{
			name:                 "gate off with both batch scheduler options — no conflict",
			featureGateEnabled:   false,
			enableBatchScheduler: true,
			batchScheduler:       "volcano",
			wantErr:              false,
		},
		// Negative cases: error expected (mutually exclusive)
		{
			name:                 "NativeWorkloadScheduling + EnableBatchScheduler — mutually exclusive",
			featureGateEnabled:   true,
			enableBatchScheduler: true,
			batchScheduler:       "",
			wantErr:              true,
			errContains:          "mutually exclusive",
		},
		{
			name:                 "NativeWorkloadScheduling + BatchScheduler=volcano — mutually exclusive",
			featureGateEnabled:   true,
			enableBatchScheduler: false,
			batchScheduler:       "volcano",
			wantErr:              true,
			errContains:          "mutually exclusive",
		},
		{
			name:                 "NativeWorkloadScheduling + BatchScheduler=yunikorn — mutually exclusive",
			featureGateEnabled:   true,
			enableBatchScheduler: false,
			batchScheduler:       "yunikorn",
			wantErr:              true,
			errContains:          "mutually exclusive",
		},
		{
			name:                 "NativeWorkloadScheduling + BatchScheduler=kai-scheduler — mutually exclusive",
			featureGateEnabled:   true,
			enableBatchScheduler: false,
			batchScheduler:       "kai-scheduler",
			wantErr:              true,
			errContains:          "mutually exclusive",
		},
		{
			name:                 "NativeWorkloadScheduling + EnableBatchScheduler + BatchScheduler — mutually exclusive",
			featureGateEnabled:   true,
			enableBatchScheduler: true,
			batchScheduler:       "volcano",
			wantErr:              true,
			errContains:          "mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, tt.featureGateEnabled)

			config := configapi.Configuration{
				EnableBatchScheduler: tt.enableBatchScheduler,
				BatchScheduler:       tt.batchScheduler,
			}

			err := validateNativeWorkloadSchedulingConfig(config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got nil")
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func Test_checkSchedulingV1alpha2Available(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantErr     bool
		errContains string
	}{
		// Positive: API is available
		{
			name: "API available — returns resource list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/apis/scheduling.k8s.io/v1alpha2" {
					w.Header().Set("Content-Type", "application/json")
					resourceList := metav1.APIResourceList{
						GroupVersion: "scheduling.k8s.io/v1alpha2",
						APIResources: []metav1.APIResource{
							{Name: "workloads", Kind: "Workload", Namespaced: true},
							{Name: "podgroups", Kind: "PodGroup", Namespaced: true},
						},
					}
					_ = json.NewEncoder(w).Encode(resourceList)
					return
				}
				http.NotFound(w, r)
			},
			wantErr: false,
		},
		{
			name: "API available — empty resource list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/apis/scheduling.k8s.io/v1alpha2" {
					w.Header().Set("Content-Type", "application/json")
					resourceList := metav1.APIResourceList{
						GroupVersion: "scheduling.k8s.io/v1alpha2",
					}
					_ = json.NewEncoder(w).Encode(resourceList)
					return
				}
				http.NotFound(w, r)
			},
			wantErr: false,
		},
		// Negative: API not available
		{
			name: "API not available — 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			},
			wantErr:     true,
			errContains: "scheduling.k8s.io/v1alpha2 API is not available",
		},
		{
			name: "API not available — 500 server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "internal server error", http.StatusInternalServerError)
			},
			wantErr:     true,
			errContains: "scheduling.k8s.io/v1alpha2 API is not available",
		},
		{
			name: "API not available — different group version exists but not v1alpha2",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/apis/scheduling.k8s.io/v1" {
					w.Header().Set("Content-Type", "application/json")
					resourceList := metav1.APIResourceList{
						GroupVersion: "scheduling.k8s.io/v1",
					}
					_ = json.NewEncoder(w).Encode(resourceList)
					return
				}
				http.NotFound(w, r)
			},
			wantErr:     true,
			errContains: "scheduling.k8s.io/v1alpha2 API is not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			restConfig := &rest.Config{
				Host: server.URL,
			}

			err := checkSchedulingV1alpha2Available(restConfig)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got nil")
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func Test_checkSchedulingV1alpha2Available_unreachableServer(t *testing.T) {
	restConfig := &rest.Config{
		Host: "http://127.0.0.1:1", // port 1 is almost certainly not listening
	}

	err := checkSchedulingV1alpha2Available(restConfig)
	if err == nil {
		t.Errorf("expected error for unreachable server but got nil")
	}
	if !strings.Contains(err.Error(), "scheduling.k8s.io/v1alpha2 API is not available") {
		t.Errorf("expected error about API not available, got: %v", err)
	}
}
