package v1

import (
	"testing"


	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsGCSFaultToleranceEnabled(t *testing.T) {
	tests := []struct {
		name     string
		instance RayCluster
		expected bool
	}{
		{
			name: "ray.io/ft-enabled is true",
			instance: RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RayFTEnabledAnnotationKey: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "ray.io/ft-enabled is not set and GcsFaultToleranceOptions is set",
			instance: RayCluster{
				Spec: RayClusterSpec{
					GcsFaultToleranceOptions: &GcsFaultToleranceOptions{},
				},
			},
			expected: true,
		},
		{
			name: "ray.io/ft-enabled is false",
			instance: RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RayFTEnabledAnnotationKey: "false",
					},
				},
			},
			expected: false,
		},
		{
			name: "ray.io/ft-enabled is not set and GcsFaultToleranceOptions is not set",
			instance: RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
		{
			name: "ray.io/ft-enabled is using uppercase true",
			instance: RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RayFTEnabledAnnotationKey: "TRUE",
					},
				},
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := IsGCSFaultToleranceEnabled(test.instance)
			assert.Equal(t, test.expected, result)
		})
	}
}
