package session

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes/fake"
)

func TestComplete(t *testing.T) {
	cmd := &cobra.Command{Use: "session"}

	tests := []struct {
		name              string
		namespace         string
		expectedNamespace string
		args              []string
		hasErr            bool
	}{
		{
			name:              "valid args without namespace",
			namespace:         "",
			args:              []string{"test-cluster"},
			expectedNamespace: "default",
			hasErr:            false,
		},
		{
			name:              "valid args with namespace",
			namespace:         "test-namespace",
			args:              []string{"test-cluster"},
			expectedNamespace: "test-namespace",
			hasErr:            false,
		},
		{
			name:              "invalid args (no args)",
			namespace:         "",
			args:              []string{},
			expectedNamespace: "",
			hasErr:            true,
		},
		{
			name:              "invalid args (too many args)",
			namespace:         "",
			args:              []string{"test-cluster", "extra-arg"},
			expectedNamespace: "",
			hasErr:            true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testStreams, _, _, _ := genericiooptions.NewTestIOStreams()
			fakeSessionOptions := NewSessionOptions(testStreams)
			fakeSessionOptions.configFlags.Namespace = &tc.namespace
			err := fakeSessionOptions.Complete(cmd, tc.args)
			if tc.hasErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tc.expectedNamespace, fakeSessionOptions.Namespace)
			}
		})
	}
}

func TestFindServiceName(t *testing.T) {
	objects := []runtime.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "raycluster-default-head-svc",
				Namespace: "default",
				Labels: map[string]string{
					"ray.io/cluster":   "raycluster-default",
					"ray.io/node-type": "head",
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "raycluster-test-head-svc",
				Namespace: "test",
				Labels: map[string]string{
					"ray.io/cluster":   "raycluster-test",
					"ray.io/node-type": "head",
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "raycluster-non-related-head-svc",
				Namespace: "test",
				Labels: map[string]string{
					"ray.io/cluster":   "raycluster-non-related",
					"ray.io/node-type": "head",
				},
			},
		},
	}

	kubeClientSet := fake.NewSimpleClientset(objects...)

	tests := []struct {
		name         string
		namespace    string
		resourceName string
		serviceName  string
	}{
		{
			name:         "find service name in default namespace",
			namespace:    "default",
			resourceName: "raycluster-default",
			serviceName:  "service/raycluster-default-head-svc",
		},
		{
			name:         "find service name in test namespace",
			namespace:    "test",
			resourceName: "raycluster-test",
			serviceName:  "service/raycluster-test-head-svc",
		},
		{
			name:         "resource not found",
			namespace:    "default",
			resourceName: "raycluster-not-found",
			serviceName:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			serviceName, err := findServiceName(context.Background(), kubeClientSet, tc.namespace, tc.resourceName)
			if tc.serviceName == "" {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tc.serviceName, serviceName)
			}
		})
	}
}
