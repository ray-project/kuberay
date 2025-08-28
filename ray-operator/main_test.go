package main

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
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
				EnableLeaderElection: ptr.To(true),
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
				EnableLeaderElection: ptr.To(true),
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
				EnableLeaderElection: ptr.To(true),
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
				EnableLeaderElection: ptr.To(true),
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
				EnableLeaderElection: ptr.To(true),
				ReconcileConcurrency: 1,
				QPS:                  ptr.To((150.5)),
				Burst:                ptr.To(300),
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
				EnableLeaderElection: ptr.To(true),
				ReconcileConcurrency: 1,
				QPS:                  ptr.To((150.0)),
				Burst:                ptr.To(300),
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
				EnableLeaderElection: ptr.To(true),
				ReconcileConcurrency: 0,
				QPS:                  ptr.To(configapi.DefaultQPS),
				Burst:                ptr.To(configapi.DefaultBurst),
			},
			expectErr:   true,
			errContains: "json: cannot unmarshal bool into Go struct field Configuration.reconcileConcurrency of type int",
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
