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

func TestValidateRayClusterSpecGcsFaultToleranceOptions(t *testing.T) {
	errorMessageBothSet := fmt.Sprintf("%s annotation and GcsFaultToleranceOptions are both set. "+
		"Please use only GcsFaultToleranceOptions to configure GCS fault tolerance", RayFTEnabledAnnotationKey)
	errorMessageRedisAddressSet := fmt.Sprintf("%s is set which implicitly enables GCS fault tolerance, "+
		"but GcsFaultToleranceOptions is not set. Please set GcsFaultToleranceOptions "+
		"to enable GCS fault tolerance", RAY_REDIS_ADDRESS)

	tests := []struct {
		gcsFaultToleranceOptions *GcsFaultToleranceOptions
		annotations              map[string]string
		name                     string
		errorMessage             string
		envVars                  []corev1.EnvVar
		expectError              bool
	}{
		// GcsFaultToleranceOptions and ray.io/ft-enabled should not be both set.
		{
			name: "ray.io/ft-enabled is set to false and GcsFaultToleranceOptions is set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "false",
			},
			gcsFaultToleranceOptions: &GcsFaultToleranceOptions{},
			expectError:              true,
			errorMessage:             errorMessageBothSet,
		},
		{
			name: "ray.io/ft-enabled is set to true and GcsFaultToleranceOptions is set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "true",
			},
			gcsFaultToleranceOptions: &GcsFaultToleranceOptions{},
			expectError:              true,
			errorMessage:             errorMessageBothSet,
		},
		{
			name:                     "ray.io/ft-enabled is not set and GcsFaultToleranceOptions is set",
			gcsFaultToleranceOptions: &GcsFaultToleranceOptions{},
			expectError:              false,
		},
		{
			name:                     "ray.io/ft-enabled is not set and GcsFaultToleranceOptions is not set",
			gcsFaultToleranceOptions: nil,
			expectError:              false,
		},
		// RAY_REDIS_ADDRESS should not be set if KubeRay is not aware that GCS fault tolerance is enabled.
		{
			name: "ray.io/ft-enabled is set to false and RAY_REDIS_ADDRESS is set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "false",
			},
			envVars: []corev1.EnvVar{
				{
					Name:  RAY_REDIS_ADDRESS,
					Value: "redis:6379",
				},
			},
			expectError:  true,
			errorMessage: errorMessageRedisAddressSet,
		},
		{
			name: "ray.io/ft-enabled is not set and RAY_REDIS_ADDRESS is set",
			envVars: []corev1.EnvVar{
				{
					Name:  RAY_REDIS_ADDRESS,
					Value: "redis:6379",
				},
			},
			expectError:  true,
			errorMessage: errorMessageRedisAddressSet,
		},
		{
			name: "ray.io/ft-enabled is set to true and RAY_REDIS_ADDRESS is set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "true",
			},
			envVars: []corev1.EnvVar{
				{
					Name:  RAY_REDIS_ADDRESS,
					Value: "redis:6379",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rayCluster := &RayCluster{
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
										Env: tt.envVars,
									},
								},
							},
						},
					},
				},
			}
			err := rayCluster.ValidateRayClusterSpec()
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

func TestValidateRayClusterSpecRedisPassword(t *testing.T) {
	tests := []struct {
		gcsFaultToleranceOptions *GcsFaultToleranceOptions
		name                     string
		rayStartParams           map[string]string
		envVars                  []corev1.EnvVar
		expectError              bool
	}{
		{
			name:                     "GcsFaultToleranceOptions is set and `redis-password` is also set in rayStartParams",
			gcsFaultToleranceOptions: &GcsFaultToleranceOptions{},
			rayStartParams: map[string]string{
				"redis-password": "password",
			},
			expectError: true,
		},
		{
			name:                     "GcsFaultToleranceOptions is set and `REDIS_PASSWORD` env var is also set in the head Pod",
			gcsFaultToleranceOptions: &GcsFaultToleranceOptions{},
			envVars: []corev1.EnvVar{
				{
					Name:  REDIS_PASSWORD,
					Value: "password",
				},
			},
			expectError: true,
		},
		{
			name: "GcsFaultToleranceOptions.RedisPassword is set",
			gcsFaultToleranceOptions: &GcsFaultToleranceOptions{
				RedisPassword: &RedisCredential{
					Value: "password",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rayCluster := &RayCluster{
				Spec: RayClusterSpec{
					GcsFaultToleranceOptions: tt.gcsFaultToleranceOptions,
					HeadGroupSpec: HeadGroupSpec{
						RayStartParams: tt.rayStartParams,
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Env: tt.envVars,
									},
								},
							},
						},
					},
				},
			}
			err := rayCluster.ValidateRayClusterSpec()
			if tt.expectError {
				assert.NotNil(t, err)
				assert.IsType(t, &field.Error{}, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestValidateRayClusterSpecEmptyContainers(t *testing.T) {
	headGroupSpecWithOneContainer := HeadGroupSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "ray-head"}},
			},
		},
	}
	workerGroupSpecWithOneContainer := WorkerGroupSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "ray-worker"}},
			},
		},
	}
	headGroupSpecWithNoContainers := *headGroupSpecWithOneContainer.DeepCopy()
	headGroupSpecWithNoContainers.Template.Spec.Containers = []corev1.Container{}
	workerGroupSpecWithNoContainers := *workerGroupSpecWithOneContainer.DeepCopy()
	workerGroupSpecWithNoContainers.Template.Spec.Containers = []corev1.Container{}

	tests := []struct {
		rayCluster   *RayCluster
		name         string
		errorMessage string
		expectError  bool
	}{
		{
			name: "headGroupSpec has no containers",
			rayCluster: &RayCluster{
				Spec: RayClusterSpec{
					HeadGroupSpec: headGroupSpecWithNoContainers,
				},
			},
			expectError:  true,
			errorMessage: "headGroupSpec should have at least one container",
		},
		{
			name: "workerGroupSpec has no containers",
			rayCluster: &RayCluster{
				Spec: RayClusterSpec{
					HeadGroupSpec:    headGroupSpecWithOneContainer,
					WorkerGroupSpecs: []WorkerGroupSpec{workerGroupSpecWithNoContainers},
				},
			},
			expectError:  true,
			errorMessage: "workerGroupSpec should have at least one container",
		},
		{
			name: "valid cluster with containers in both head and worker groups",
			rayCluster: &RayCluster{
				Spec: RayClusterSpec{
					HeadGroupSpec:    headGroupSpecWithOneContainer,
					WorkerGroupSpecs: []WorkerGroupSpec{workerGroupSpecWithOneContainer},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rayCluster.ValidateRayClusterSpec()
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
	validHeadGroupSpec := HeadGroupSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "ray-head"},
				},
			},
		},
	}
	workerGroupSpec := WorkerGroupSpec{
		GroupName: "worker-group-1",
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "ray-worker"},
				},
			},
		},
	}
	workerGroupSpecs := []WorkerGroupSpec{workerGroupSpec}

	tests := []struct {
		rayCluster   *RayCluster
		name         string
		errorMessage string
		expectError  bool
	}{
		{
			name: "valid RayCluster",
			rayCluster: &RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-name",
				},
				Spec: RayClusterSpec{
					HeadGroupSpec:    validHeadGroupSpec,
					WorkerGroupSpecs: workerGroupSpecs,
				},
			},
			expectError: false,
		},
		{
			name: "invalid rayCluster name",
			rayCluster: &RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Invalid_Name",
				},
				Spec: RayClusterSpec{
					HeadGroupSpec:    validHeadGroupSpec,
					WorkerGroupSpecs: workerGroupSpecs,
				},
			},
			expectError:  true,
			errorMessage: "name must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character",
		},
		{
			name: "duplicate worker group names",
			rayCluster: &RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-name",
				},
				Spec: RayClusterSpec{
					HeadGroupSpec: validHeadGroupSpec,
					WorkerGroupSpecs: []WorkerGroupSpec{
						workerGroupSpec,
						workerGroupSpec,
					},
				},
			},
			expectError:  true,
			errorMessage: "worker group names must be unique",
		},
		{
			name: "headGroupSpec has no containers",
			rayCluster: &RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-name",
				},
				Spec: RayClusterSpec{
					HeadGroupSpec: HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{},
							},
						},
					},
					WorkerGroupSpecs: workerGroupSpecs,
				},
			},
			expectError:  true,
			errorMessage: "headGroupSpec should have at least one container",
		},
		{
			name: "workerGroupSpec has no containers",
			rayCluster: &RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-name",
				},
				Spec: RayClusterSpec{
					HeadGroupSpec: validHeadGroupSpec,
					WorkerGroupSpecs: []WorkerGroupSpec{
						{
							GroupName: "worker-group-1",
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{},
								},
							},
						},
					},
				},
			},
			expectError:  true,
			errorMessage: "workerGroupSpec should have at least one container",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rayCluster.validateRayCluster()
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
