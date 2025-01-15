package v1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestEnvVarExists(t *testing.T) {
	tests := []struct {
		name     string
		envName  string
		envVars  []corev1.EnvVar
		expected bool
	}{
		{
			name:    "env var exists",
			envName: "EXISTING_ENV",
			envVars: []corev1.EnvVar{
				{Name: "EXISTING_ENV", Value: "value1"},
				{Name: "ANOTHER_ENV", Value: "value2"},
			},
			expected: true,
		},
		{
			name:    "env var does not exist",
			envName: "NON_EXISTING_ENV",
			envVars: []corev1.EnvVar{
				{Name: "EXISTING_ENV", Value: "value1"},
				{Name: "ANOTHER_ENV", Value: "value2"},
			},
			expected: false,
		},
		{
			name:     "empty env vars",
			envName:  "ANY_ENV",
			envVars:  []corev1.EnvVar{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EnvVarExists(tt.envName, tt.envVars)
			if result != tt.expected {
				t.Errorf("EnvVarExists(%s, %v) = %v; expected %v", tt.envName, tt.envVars, result, tt.expected)
			}
		})
	}
}
