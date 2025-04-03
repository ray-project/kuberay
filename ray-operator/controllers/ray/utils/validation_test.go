package utils

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

func TestValidateRayClusterStatus(t *testing.T) {
	tests := []struct {
		name        string
		conditions  []metav1.Condition
		expectError bool
	}{
		{
			name: "Both suspending and suspended are true",
			conditions: []metav1.Condition{
				{
					Type:   string(rayv1.RayClusterSuspending),
					Status: metav1.ConditionTrue,
				},
				{
					Type:   string(rayv1.RayClusterSuspended),
					Status: metav1.ConditionTrue,
				},
			},
			expectError: true,
		},
		{
			name: "Only suspending is true",
			conditions: []metav1.Condition{
				{
					Type:   string(rayv1.RayClusterSuspending),
					Status: metav1.ConditionTrue,
				},
				{
					Type:   string(rayv1.RayClusterSuspended),
					Status: metav1.ConditionFalse,
				},
			},
			expectError: false,
		},
		{
			name: "Only suspended is true",
			conditions: []metav1.Condition{
				{
					Type:   string(rayv1.RayClusterSuspending),
					Status: metav1.ConditionFalse,
				},
				{
					Type:   string(rayv1.RayClusterSuspended),
					Status: metav1.ConditionTrue,
				},
			},
			expectError: false,
		},
		{
			name: "Both suspending and suspended are false",
			conditions: []metav1.Condition{
				{
					Type:   string(rayv1.RayClusterSuspending),
					Status: metav1.ConditionFalse,
				},
				{
					Type:   string(rayv1.RayClusterSuspended),
					Status: metav1.ConditionFalse,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &rayv1.RayCluster{
				Status: rayv1.RayClusterStatus{
					Conditions: tt.conditions,
				},
			}
			err := ValidateRayClusterStatus(instance)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateRayClusterStatus() error = %v, wantErr %v", err, tt.expectError)
			}
		})
	}
}

func TestValidateRayClusterSpecGcsFaultToleranceOptions(t *testing.T) {
	errorMessageBothSet := fmt.Sprintf("%s annotation and GcsFaultToleranceOptions are both set. "+
		"Please use only GcsFaultToleranceOptions to configure GCS fault tolerance", RayFTEnabledAnnotationKey)
	errorMessageRedisAddressSet := fmt.Sprintf("%s is set which implicitly enables GCS fault tolerance, "+
		"but GcsFaultToleranceOptions is not set. Please set GcsFaultToleranceOptions "+
		"to enable GCS fault tolerance", RAY_REDIS_ADDRESS)
	errorMessageRedisAddressConflict := fmt.Sprintf("cannot set `%s` env var in head Pod when "+
		"GcsFaultToleranceOptions is enabled - use GcsFaultToleranceOptions.RedisAddress instead", RAY_REDIS_ADDRESS)
	errorMessageExternalStorageNamespaceConflict := fmt.Sprintf("cannot set `%s` annotation when "+
		"GcsFaultToleranceOptions is enabled - use GcsFaultToleranceOptions.ExternalStorageNamespace instead", RayExternalStorageNSAnnotationKey)

	tests := []struct {
		rayStartParams           map[string]string
		gcsFaultToleranceOptions *rayv1.GcsFaultToleranceOptions
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
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{},
			expectError:              true,
			errorMessage:             errorMessageBothSet,
		},
		{
			name: "ray.io/ft-enabled is set to true and GcsFaultToleranceOptions is set",
			annotations: map[string]string{
				RayFTEnabledAnnotationKey: "true",
			},
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{},
			expectError:              true,
			errorMessage:             errorMessageBothSet,
		},
		{
			name:                     "ray.io/ft-enabled is not set and GcsFaultToleranceOptions is set",
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{},
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
			name: "gcsFaultToleranceOptions is set and RAY_REDIS_ADDRESS is set",
			envVars: []corev1.EnvVar{
				{
					Name:  RAY_REDIS_ADDRESS,
					Value: "redis:6379",
				},
			},
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{},
			expectError:              true,
			errorMessage:             errorMessageRedisAddressConflict,
		},
		{
			name: "FT is disabled and RAY_REDIS_ADDRESS is set",
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
		{
			name: "gcsFaultToleranceOptions is set and ray.io/external-storage-namespace is set",
			annotations: map[string]string{
				RayExternalStorageNSAnnotationKey: "myns",
			},
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{},
			expectError:              true,
			errorMessage:             errorMessageExternalStorageNamespaceConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRayClusterSpec(&rayv1.RayClusterSpec{
				GcsFaultToleranceOptions: tt.gcsFaultToleranceOptions,
				HeadGroupSpec: rayv1.HeadGroupSpec{
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
			}, tt.annotations)
			if tt.expectError {
				require.Error(t, err)
				assert.EqualError(t, err, tt.errorMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRayClusterSpecRedisPassword(t *testing.T) {
	tests := []struct {
		gcsFaultToleranceOptions *rayv1.GcsFaultToleranceOptions
		name                     string
		rayStartParams           map[string]string
		envVars                  []corev1.EnvVar
		expectError              bool
	}{
		{
			name:                     "GcsFaultToleranceOptions is set and `redis-password` is also set in rayStartParams",
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{},
			rayStartParams: map[string]string{
				"redis-password": "password",
			},
			expectError: true,
		},
		{
			name:                     "GcsFaultToleranceOptions is set and `REDIS_PASSWORD` env var is also set in the head Pod",
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{},
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
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{
				RedisPassword: &rayv1.RedisCredential{
					Value: "password",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rayCluster := &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					GcsFaultToleranceOptions: tt.gcsFaultToleranceOptions,
					HeadGroupSpec: rayv1.HeadGroupSpec{
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
			err := ValidateRayClusterSpec(&rayCluster.Spec, rayCluster.Annotations)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRayClusterSpecRedisUsername(t *testing.T) {
	errorMessageRedisUsername := "cannot set redis username in rayStartParams or environment variables - use GcsFaultToleranceOptions.RedisUsername instead"

	tests := []struct {
		gcsFaultToleranceOptions *rayv1.GcsFaultToleranceOptions
		name                     string
		errorMessage             string
		rayStartParams           map[string]string
		envVars                  []corev1.EnvVar
		expectError              bool
	}{
		{
			name: "`redis-username` is set in rayStartParams of the Head Pod",
			rayStartParams: map[string]string{
				"redis-username": "username",
			},
			expectError:  true,
			errorMessage: errorMessageRedisUsername,
		},
		{
			name: "`REDIS_USERNAME` env var is set in the Head Pod",
			envVars: []corev1.EnvVar{
				{
					Name:  REDIS_USERNAME,
					Value: "username",
				},
			},
			expectError:  true,
			errorMessage: errorMessageRedisUsername,
		},
		{
			name: "GcsFaultToleranceOptions.RedisUsername is set",
			gcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{
				RedisUsername: &rayv1.RedisCredential{
					Value: "username",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rayCluster := &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					GcsFaultToleranceOptions: tt.gcsFaultToleranceOptions,
					HeadGroupSpec: rayv1.HeadGroupSpec{
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
			err := ValidateRayClusterSpec(&rayCluster.Spec, rayCluster.Annotations)
			if tt.expectError {
				require.Error(t, err)
				assert.EqualError(t, err, tt.errorMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRayClusterSpecNames(t *testing.T) {
	tests := []struct {
		name         string
		errorMessage string
		metadata     metav1.ObjectMeta
		expectError  bool
	}{
		{
			name: "RayCluster name is too long (> MaxRayClusterNameLength characters)",
			metadata: metav1.ObjectMeta{
				Name: strings.Repeat("a", MaxRayClusterNameLength+1),
			},
			expectError:  true,
			errorMessage: fmt.Sprintf("RayCluster name should be no more than %d characters", MaxRayClusterNameLength),
		},
		{
			name: "RayCluster name is ok (== MaxRayClusterNameLength)",
			metadata: metav1.ObjectMeta{
				Name: strings.Repeat("a", MaxRayClusterNameLength),
			},
			expectError: false,
		},
		{
			name: "RayCluster name is not a DNS1035 label",
			metadata: metav1.ObjectMeta{
				Name: strings.Repeat("1", MaxRayClusterNameLength),
			},
			expectError:  true,
			errorMessage: "RayCluster name should be a valid DNS1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRayClusterMetadata(tt.metadata)
			if tt.expectError {
				require.Error(t, err)
				assert.EqualError(t, err, tt.errorMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRayClusterSpecEmptyContainers(t *testing.T) {
	headGroupSpecWithOneContainer := rayv1.HeadGroupSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "ray-head"}},
			},
		},
	}
	workerGroupSpecWithOneContainer := rayv1.WorkerGroupSpec{
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
		rayCluster   *rayv1.RayCluster
		name         string
		errorMessage string
		expectError  bool
	}{
		{
			name: "headGroupSpec has no containers",
			rayCluster: &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: headGroupSpecWithNoContainers,
				},
			},
			expectError:  true,
			errorMessage: "headGroupSpec should have at least one container",
		},
		{
			name: "workerGroupSpec has no containers",
			rayCluster: &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec:    headGroupSpecWithOneContainer,
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{workerGroupSpecWithNoContainers},
				},
			},
			expectError:  true,
			errorMessage: "workerGroupSpec should have at least one container",
		},
		{
			name: "valid cluster with containers in both head and worker groups",
			rayCluster: &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec:    headGroupSpecWithOneContainer,
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{workerGroupSpecWithOneContainer},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRayClusterSpec(&tt.rayCluster.Spec, tt.rayCluster.Annotations)
			if tt.expectError {
				require.Error(t, err)
				assert.EqualError(t, err, tt.errorMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRayClusterSpecSuspendingWorkerGroup(t *testing.T) {
	headGroupSpec := rayv1.HeadGroupSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "ray-head"}},
			},
		},
	}
	workerGroupSpecSuspended := rayv1.WorkerGroupSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "ray-worker"}},
			},
		},
	}
	workerGroupSpecSuspended.Suspend = ptr.To[bool](true)

	tests := []struct {
		rayCluster   *rayv1.RayCluster
		name         string
		errorMessage string
		expectError  bool
		featureGate  bool
	}{
		{
			name: "suspend without autoscaler and the feature gate",
			rayCluster: &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec:    headGroupSpec,
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{workerGroupSpecSuspended},
				},
			},
			featureGate:  false,
			expectError:  true,
			errorMessage: "suspending worker groups is currently available when the RayJobDeletionPolicy feature gate is enabled",
		},
		{
			name: "suspend without autoscaler",
			rayCluster: &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec:    headGroupSpec,
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{workerGroupSpecSuspended},
				},
			},
			featureGate: true,
			expectError: false,
		},
		{
			// TODO (rueian): This can be supported in future Ray. We should check the RayVersion once we know the version.
			name: "suspend with autoscaler",
			rayCluster: &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec:           headGroupSpec,
					WorkerGroupSpecs:        []rayv1.WorkerGroupSpec{workerGroupSpecSuspended},
					EnableInTreeAutoscaling: ptr.To[bool](true),
				},
			},
			featureGate:  true,
			expectError:  true,
			errorMessage: "suspending worker groups is not currently supported with Autoscaler enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features.SetFeatureGateDuringTest(t, features.RayJobDeletionPolicy, tt.featureGate)
			err := ValidateRayClusterSpec(&tt.rayCluster.Spec, tt.rayCluster.Annotations)
			if tt.expectError {
				require.Error(t, err)
				assert.EqualError(t, err, tt.errorMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRayJobStatus(t *testing.T) {
	tests := []struct {
		name        string
		jobSpec     rayv1.RayJobSpec
		jobStatus   rayv1.RayJobStatus
		expectError bool
	}{
		{
			name: "JobDeploymentStatus is Waiting and SubmissionMode is not InteractiveMode",
			jobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusWaiting,
			},
			jobSpec: rayv1.RayJobSpec{
				SubmissionMode: rayv1.K8sJobMode,
			},
			expectError: true,
		},
		{
			name: "JobDeploymentStatus is Waiting and SubmissionMode is InteractiveMode",
			jobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusWaiting,
			},
			jobSpec: rayv1.RayJobSpec{
				SubmissionMode: rayv1.InteractiveMode,
			},
			expectError: false,
		},
		{
			name: "JobDeploymentStatus is not Waiting and SubmissionMode is not InteractiveMode",
			jobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusRunning,
			},
			jobSpec: rayv1.RayJobSpec{
				SubmissionMode: rayv1.HTTPMode,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rayJob := &rayv1.RayJob{
				Status: tt.jobStatus,
				Spec:   tt.jobSpec,
			}
			err := ValidateRayJobStatus(rayJob)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateRayJobStatus() error = %v, wantErr %v", err, tt.expectError)
			}
		})
	}
}

func TestValidateRayJobSpec(t *testing.T) {
	tests := []struct {
		name        string
		spec        rayv1.RayJobSpec
		expectError bool
	}{
		{
			name:        "one of RayClusterSpec or ClusterSelector must be set",
			spec:        rayv1.RayJobSpec{},
			expectError: true,
		},
		{
			name: "a RayJob with shutdownAfterJobFinishes set to false is not allowed to be suspended",
			spec: rayv1.RayJobSpec{
				Suspend:                  true,
				ShutdownAfterJobFinishes: false,
			},
			expectError: true,
		},
		{
			name: "valid RayJob",
			spec: rayv1.RayJobSpec{
				Suspend:                  true,
				ShutdownAfterJobFinishes: true,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "the ClusterSelector mode doesn't support the suspend operation",
			spec: rayv1.RayJobSpec{
				Suspend:                  true,
				ShutdownAfterJobFinishes: true,
				ClusterSelector: map[string]string{
					"key": "value",
				},
			},
			expectError: true,
		},
		{
			name: "failed to unmarshal RuntimeEnvYAML",
			spec: rayv1.RayJobSpec{
				RuntimeEnvYAML: "invalid_yaml_str",
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "backoffLimit must be a positive integer",
			spec: rayv1.RayJobSpec{
				BackoffLimit:   ptr.To[int32](-1),
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "RayJobDeletionPolicy feature gate must be enabled to use the DeletionPolicy feature",
			spec: rayv1.RayJobSpec{
				DeletionPolicy:           ptr.To(rayv1.DeleteClusterDeletionPolicy),
				ShutdownAfterJobFinishes: true,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRayJobSpec(&rayv1.RayJob{
				Spec: tt.spec,
			})
			if tt.expectError {
				require.Error(t, err, tt.name)
			} else {
				require.NoError(t, err, tt.name)
			}
		})
	}
}

func TestValidateRayJobSpecWithFeatureGate(t *testing.T) {
	headGroupSpecWithOneContainer := rayv1.HeadGroupSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "ray-head"}},
			},
		},
	}

	tests := []struct {
		name        string
		spec        rayv1.RayJobSpec
		expectError bool
	}{
		{
			name: "the ClusterSelector mode doesn't support DeletionPolicy=DeleteCluster",
			spec: rayv1.RayJobSpec{
				DeletionPolicy:  ptr.To(rayv1.DeleteClusterDeletionPolicy),
				ClusterSelector: map[string]string{"key": "value"},
			},
			expectError: true,
		},
		{
			name: "the ClusterSelector mode doesn't support DeletionPolicy=DeleteWorkers",
			spec: rayv1.RayJobSpec{
				DeletionPolicy:  ptr.To(rayv1.DeleteWorkersDeletionPolicy),
				ClusterSelector: map[string]string{"key": "value"},
			},
			expectError: true,
		},
		{
			name: "DeletionPolicy=DeleteWorkers currently does not support RayCluster with autoscaling enabled",
			spec: rayv1.RayJobSpec{
				DeletionPolicy: ptr.To(rayv1.DeleteWorkersDeletionPolicy),
				RayClusterSpec: &rayv1.RayClusterSpec{
					EnableInTreeAutoscaling: ptr.To[bool](true),
					HeadGroupSpec:           headGroupSpecWithOneContainer,
				},
			},
			expectError: true,
		},
		{
			name: "valid RayJob with DeletionPolicy=DeleteCluster",
			spec: rayv1.RayJobSpec{
				DeletionPolicy:           ptr.To(rayv1.DeleteClusterDeletionPolicy),
				ShutdownAfterJobFinishes: true,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "valid RayJob without DeletionPolicy",
			spec: rayv1.RayJobSpec{
				DeletionPolicy:           nil,
				ShutdownAfterJobFinishes: true,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "shutdownAfterJobFinshes is set to 'true' while deletion policy is 'DeleteNone'",
			spec: rayv1.RayJobSpec{
				DeletionPolicy:           ptr.To(rayv1.DeleteNoneDeletionPolicy),
				ShutdownAfterJobFinishes: true,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "headGroupSpec should have at least one container",
			spec: rayv1.RayJobSpec{
				RayClusterSpec: &rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{},
				},
			},
			expectError: true,
		},
	}

	features.SetFeatureGateDuringTest(t, features.RayJobDeletionPolicy, true)
	defer func() {
		features.SetFeatureGateDuringTest(t, features.RayJobDeletionPolicy, false)
	}()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRayJobSpec(&rayv1.RayJob{
				Spec: tt.spec,
			})
			if tt.expectError {
				require.Error(t, err, tt.name)
			} else {
				require.NoError(t, err, tt.name)
			}
		})
	}
}

func TestValidateRayJobMetadata(t *testing.T) {
	err := ValidateRayJobMetadata(metav1.ObjectMeta{
		Name: strings.Repeat("j", MaxRayJobNameLength+1),
	})
	require.ErrorContains(t, err, fmt.Sprintf("RayJob name should be no more than %d characters", MaxRayJobNameLength))

	err = ValidateRayJobMetadata(metav1.ObjectMeta{
		Name: strings.Repeat("1", MaxRayJobNameLength),
	})
	require.ErrorContains(t, err, "RayJob name should be a valid DNS1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]")

	err = ValidateRayJobMetadata(metav1.ObjectMeta{
		Name: strings.Repeat("j", MaxRayJobNameLength),
	})
	require.NoError(t, err)
}

func TestValidateRayServiceSpec(t *testing.T) {
	upgradeStrat := rayv1.RayServiceUpgradeType("invalidStrategy")

	tests := []struct {
		name              string
		unexpectedMessage string
		spec              rayv1.RayServiceSpec
		expectError       bool
	}{
		{
			name: "spec.rayClusterConfig.headGroupSpec.headService.metadata.name should not be set",
			spec: rayv1.RayServiceSpec{
				RayClusterSpec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						HeadService: &corev1.Service{
							ObjectMeta: metav1.ObjectMeta{
								Name: "my-head-service",
							},
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "The RayService spec is valid.",
			spec: rayv1.RayServiceSpec{
				RayClusterSpec: *createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "spec.UpgradeSpec.Type is invalid",
			spec: rayv1.RayServiceSpec{
				UpgradeStrategy: &rayv1.RayServiceUpgradeStrategy{
					Type: &upgradeStrat,
				},
				RayClusterSpec: *createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name:        "headGroupSpec should have at least one container",
			spec:        rayv1.RayServiceSpec{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRayServiceSpec(&rayv1.RayService{
				Spec: tt.spec,
			})
			if tt.expectError {
				require.Error(t, err, tt.name)
			} else {
				require.NoError(t, err, tt.name)
			}
		})
	}
}

func TestValidateRayServiceMetadata(t *testing.T) {
	err := ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: strings.Repeat("j", MaxRayServiceNameLength+1),
	})
	require.ErrorContains(t, err, fmt.Sprintf("RayService name should be no more than %d characters", MaxRayServiceNameLength))

	err = ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: strings.Repeat("1", MaxRayServiceNameLength),
	})
	require.ErrorContains(t, err, "RayService name should be a valid DNS1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]")

	err = ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: strings.Repeat("j", MaxRayServiceNameLength),
	})
	require.NoError(t, err)
}

func createBasicRayClusterSpec() *rayv1.RayClusterSpec {
	return &rayv1.RayClusterSpec{
		HeadGroupSpec: rayv1.HeadGroupSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "ray-head"},
					},
				},
			},
		},
	}
}
