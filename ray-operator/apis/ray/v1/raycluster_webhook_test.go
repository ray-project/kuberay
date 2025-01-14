package v1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestValidateRayClusterSpec(t *testing.T) {
	tests := []struct {
		gcsFaultToleranceOptions *GcsFaultToleranceOptions
		annotations              map[string]string
		name                     string
		errorMessage             string
		envVars                  []corev1.EnvVar
		expectError              bool
	}{
		{
			name: "FT disabled with GcsFaultToleranceOptions set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "false",
			},
			gcsFaultToleranceOptions: &GcsFaultToleranceOptions{},
			expectError:              true,
			errorMessage:             fmt.Sprintf("GcsFaultToleranceOptions should be nil when %s annotation is set to false", RayFTEnabledAnnotationKey),
		},
		{
			name: "FT disabled with RAY_REDIS_ADDRESS set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "false",
			},
			envVars: []corev1.EnvVar{
				{
					Name:  RAY_REDIS_ADDRESS,
					Value: "redis://127.0.0.1:6379",
				},
			},
			expectError:  true,
			errorMessage: fmt.Sprintf("%s should not be set when %s is disabled", RAY_REDIS_ADDRESS, RayFTEnabledAnnotationKey),
		},
		{
			name:        "FT not set with RAY_REDIS_ADDRESS set",
			annotations: map[string]string{},
			envVars: []corev1.EnvVar{
				{
					Name:  RAY_REDIS_ADDRESS,
					Value: "redis://127.0.0.1:6379",
				},
			},
			expectError:  true,
			errorMessage: fmt.Sprintf("%s should not be set when %s is disabled", RAY_REDIS_ADDRESS, RayFTEnabledAnnotationKey),
		},
		{
			name: "FT disabled with other environment variables set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "false",
			},
			envVars: []corev1.EnvVar{
				{
					Name:  "SOME_OTHER_ENV",
					Value: "some-value",
				},
			},
			expectError: false,
		},
		{
			name: "FT enabled, GcsFaultToleranceOptions not nil",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "true",
			},
			gcsFaultToleranceOptions: &GcsFaultToleranceOptions{
				RedisAddress: "redis://127.0.0.1:6379",
			},
			expectError: false,
		},
		{
			name: "FT enabled, GcsFaultToleranceOptions is nil",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "true",
			},
			expectError: false,
		},
		{
			name: "FT enabled with with other environment variables set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "true",
			},
			envVars: []corev1.EnvVar{
				{
					Name:  "SOME_OTHER_ENV",
					Value: "some-value",
				},
			},
			expectError: false,
		},
		{
			name: "FT enabled with RAY_REDIS_ADDRESS set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "true",
			},
			envVars: []corev1.EnvVar{
				{
					Name:  RAY_REDIS_ADDRESS,
					Value: "redis://127.0.0.1:6379",
				},
			},
			expectError: false,
		},
		{
			name: "FT disabled with no GcsFaultToleranceOptions and no RAY_REDIS_ADDRESS",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "false",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
				Spec: RayClusterSpec{
					GcsFaultToleranceOptions: tt.gcsFaultToleranceOptions,
					HeadGroupSpec: HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name: "ray-head",
										Env:  tt.envVars,
									},
								},
							},
						},
					},
				},
			}
			err := r.ValidateRayClusterSpec()
			if tt.expectError {
				assert.NotNil(t, err)
				assert.IsType(t, &field.Error{}, err)
				assert.Equal(t, err.Detail, tt.errorMessage)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestValidateRayCluster(t *testing.T) {
	tests := []struct {
		GcsFaultToleranceOptions *GcsFaultToleranceOptions
		name                     string
		errorMessage             string
		ObjectMeta               metav1.ObjectMeta
		WorkerGroupSpecs         []WorkerGroupSpec
		expectError              bool
	}{
		{
			name: "Invalid name",
			ObjectMeta: metav1.ObjectMeta{
				Name: "Invalid_Name",
			},
			expectError:  true,
			errorMessage: "name must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character",
		},
		{
			name: "Duplicate worker group names",

			ObjectMeta: metav1.ObjectMeta{
				Name: "valid-name",
			},

			WorkerGroupSpecs: []WorkerGroupSpec{
				{GroupName: "group1"},
				{GroupName: "group1"},
			},

			expectError:  true,
			errorMessage: "worker group names must be unique",
		},
		{
			name: "FT disabled with GcsFaultToleranceOptions set",

			ObjectMeta: metav1.ObjectMeta{
				Name: "valid-name",
				Annotations: map[string]string{
					RayFTEnabledAnnotationKey: "false",
				},
			},
			GcsFaultToleranceOptions: &GcsFaultToleranceOptions{},
			expectError:              true,
			errorMessage:             fmt.Sprintf("GcsFaultToleranceOptions should be nil when %s annotation is set to false", RayFTEnabledAnnotationKey),
		},
		{
			name: "Valid RayCluster",

			ObjectMeta: metav1.ObjectMeta{
				Name: "valid-name",
				Annotations: map[string]string{
					RayFTEnabledAnnotationKey: "true",
				},
			},
			GcsFaultToleranceOptions: &GcsFaultToleranceOptions{
				RedisAddress: "redis://127.0.0.1:6379",
			},
			WorkerGroupSpecs: []WorkerGroupSpec{
				{GroupName: "group1"},
				{GroupName: "group2"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rayCluster := &RayCluster{
				ObjectMeta: tt.ObjectMeta,
				Spec: RayClusterSpec{
					GcsFaultToleranceOptions: tt.GcsFaultToleranceOptions,
					HeadGroupSpec: HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name: "ray-head",
									},
								},
							},
						},
					},
					WorkerGroupSpecs: tt.WorkerGroupSpecs,
				},
			}
			err := rayCluster.validateRayCluster()
			if tt.expectError {
				assert.NotNil(t, err)
				assert.IsType(t, &apierrors.StatusError{}, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
