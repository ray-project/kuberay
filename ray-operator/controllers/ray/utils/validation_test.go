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
					Template:       podTemplateSpec(tt.envVars, nil),
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
						Template:       podTemplateSpec(tt.envVars, nil),
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
						Template:       podTemplateSpec(tt.envVars, nil),
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
		Template: podTemplateSpec(nil, nil),
	}
	workerGroupSpecWithOneContainer := rayv1.WorkerGroupSpec{
		Template: podTemplateSpec(nil, nil),
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
		Template: podTemplateSpec(nil, nil),
	}
	workerGroupSpecSuspended := rayv1.WorkerGroupSpec{
		GroupName: "worker-group-1",
		Template:  podTemplateSpec(nil, nil),
	}
	workerGroupSpecSuspended.Suspend = ptr.To(true)

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
			errorMessage: fmt.Sprintf("worker group %s can be suspended only when the RayJobDeletionPolicy feature gate is enabled", workerGroupSpecSuspended.GroupName),
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
					EnableInTreeAutoscaling: ptr.To(true),
				},
			},
			featureGate:  true,
			expectError:  true,
			errorMessage: fmt.Sprintf("worker group %s cannot be suspended with Autoscaler enabled", workerGroupSpecSuspended.GroupName),
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

func podTemplateSpec(envVars []corev1.EnvVar, restartPolicy *corev1.RestartPolicy) corev1.PodTemplateSpec {
	spec := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Env: envVars,
				},
			},
		},
	}

	if restartPolicy != nil {
		spec.Spec.RestartPolicy = *restartPolicy
	}

	return spec
}

func TestValidateRayClusterSpecAutoscaler(t *testing.T) {
	tests := map[string]struct {
		expectedErr string
		spec        rayv1.RayClusterSpec
	}{
		"should return error if autoscaler is enabled and any worker group is suspended": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName: "worker-group-1",
						Template:  podTemplateSpec(nil, nil),
						Suspend:   ptr.To(true),
					},
				},
			},
			expectedErr: "worker group worker-group-1 cannot be suspended with Autoscaler enabled",
		},
		fmt.Sprintf("should return error if %s env var is set to '1' when autoscaler is disabled", RAY_ENABLE_AUTOSCALER_V2): {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(false),
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec([]corev1.EnvVar{
						{
							Name:  RAY_ENABLE_AUTOSCALER_V2,
							Value: "1",
						},
					}, nil),
				},
			},
			expectedErr: fmt.Sprintf("environment variable %s cannot be set to '1' when enableInTreeAutoscaling is false. Please set enableInTreeAutoscaling: true to use autoscaler v2", RAY_ENABLE_AUTOSCALER_V2),
		},
		fmt.Sprintf("should return error if %s env var is set to 'true' when autoscaler is disabled", RAY_ENABLE_AUTOSCALER_V2): {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(false),
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec([]corev1.EnvVar{
						{
							Name:  RAY_ENABLE_AUTOSCALER_V2,
							Value: "true",
						},
					}, nil),
				},
			},
			expectedErr: fmt.Sprintf("environment variable %s cannot be set to 'true' when enableInTreeAutoscaling is false. Please set enableInTreeAutoscaling: true to use autoscaler v2", RAY_ENABLE_AUTOSCALER_V2),
		},
		fmt.Sprintf("should return error if autoscaler v2 is enabled and head Pod has env var %s", RAY_ENABLE_AUTOSCALER_V2): {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV2),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec([]corev1.EnvVar{
						{
							Name:  RAY_ENABLE_AUTOSCALER_V2,
							Value: "true",
						},
					}, nil),
				},
			},
			expectedErr: fmt.Sprintf("both .spec.autoscalerOptions.version and head Pod env var %s are set, please only use the former", RAY_ENABLE_AUTOSCALER_V2),
		},
		"should return error if autoscaler v2 is enabled and head Pod has a restartPolicy other than Never or unset": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV2),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, ptr.To(corev1.RestartPolicyAlways)),
				},
			},
			expectedErr: "restartPolicy for head Pod should be Never or unset when using autoscaler V2",
		},
		"should return error if autoscaler v2 is enabled and a worker group has a restartPolicy other than Never or unset": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV2),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, ptr.To(corev1.RestartPolicyNever)),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName: "worker-group-1",
						Template:  podTemplateSpec(nil, ptr.To(corev1.RestartPolicyNever)),
					},
					{
						GroupName: "worker-group-2",
						Template:  podTemplateSpec(nil, ptr.To(corev1.RestartPolicyAlways)),
					},
				},
			},
			expectedErr: "restartPolicy for worker group worker-group-2 should be Never or unset when using autoscaler V2",
		},
		"should not return error if autoscaler configs are valid": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV2),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName: "worker-group-1",
						Template:  podTemplateSpec(nil, nil),
					},
					{
						GroupName: "worker-group-2",
						Template:  podTemplateSpec(nil, ptr.To(corev1.RestartPolicyNever)),
					},
				},
			},
		},
	}

	features.SetFeatureGateDuringTest(t, features.RayJobDeletionPolicy, true)
	defer func() {
		features.SetFeatureGateDuringTest(t, features.RayJobDeletionPolicy, false)
	}()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateRayClusterSpec(&tc.spec, map[string]string{})
			if tc.expectedErr != "" {
				require.Error(t, err)
				require.EqualError(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRayClusterSpec_Resources(t *testing.T) {
	// Util function to create a RayCluster spec.
	createSpec := func() rayv1.RayClusterSpec {
		return rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: podTemplateSpec(nil, nil),
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					GroupName: "worker-group",
					Template:  podTemplateSpec(nil, nil),
				},
			},
		}
	}

	tests := []struct {
		name         string
		errorMessage string
		spec         rayv1.RayClusterSpec
		expectError  bool
	}{
		{
			name: "Invalid: Head group has resources in both rayStartParams and top-level Resources",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.HeadGroupSpec.RayStartParams = map[string]string{"num-cpus": "1"}
				s.HeadGroupSpec.Resources = map[string]string{"CPU": "1"}
				return s
			}(),
			expectError:  true,
			errorMessage: "resource fields should not be set in both rayStartParams and Resources for Head group; please use only one",
		},
		{
			name: "Invalid: Worker group has resources in both rayStartParams and .Resources",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.WorkerGroupSpecs[0].RayStartParams = map[string]string{"num-gpus": "1"}
				s.WorkerGroupSpecs[0].Resources = map[string]string{"GPU": "1"}
				return s
			}(),
			expectError:  true,
			errorMessage: "resource fields should not be set in both rayStartParams and Resources for worker-group group; please use only one",
		},
		{
			name: "Valid: Only rayStartParams resources are set for head",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.HeadGroupSpec.RayStartParams = map[string]string{
					"num-cpus":  "2",
					"memory":    "4G",
					"resources": "{\"TPU\": \"8\"}",
				}
				return s
			}(),
			expectError: false,
		},
		{
			name: "Valid: Only .Resources field is set for worker",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.WorkerGroupSpecs[0].Resources = map[string]string{"CPU": "2", "memory": "4G", "TPU": "8"}
				return s
			}(),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRayClusterSpec(&tt.spec, nil)
			if tt.expectError {
				require.Error(t, err)
				assert.EqualError(t, err, tt.errorMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRayClusterSpec_Labels(t *testing.T) {
	// Util function to create a RayCluster spec.
	createSpec := func() rayv1.RayClusterSpec {
		return rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: podTemplateSpec(nil, nil),
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					GroupName: "worker-group",
					Template:  podTemplateSpec(nil, nil),
				},
			},
		}
	}

	tests := []struct {
		name         string
		errorMessage string
		spec         rayv1.RayClusterSpec
		expectError  bool
	}{
		{
			name: "Invalid: Head group has 'labels' in rayStartParams",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.HeadGroupSpec.RayStartParams = map[string]string{"labels": "ray.io/node-group=worker-group-1"}
				return s
			}(),
			expectError:  true,
			errorMessage: "rayStartParams['labels'] is not supported for Head group; please use the top-level Labels field instead",
		},
		{
			name: "Invalid: Worker group has 'labels' in rayStartParams",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.WorkerGroupSpecs[0].RayStartParams = map[string]string{"labels": "ray.io/node-group=worker-group-1"}
				return s
			}(),
			expectError:  true,
			errorMessage: "rayStartParams['labels'] is not supported for worker-group group; please use the top-level Labels field instead",
		},
		{
			name: "Valid: Only .Labels field is set",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.HeadGroupSpec.Labels = map[string]string{"ray.io/market-type": "on-demand"}
				s.WorkerGroupSpecs[0].Labels = map[string]string{"ray.io/accelerator-type": "TPU-V6E"}
				return s
			}(),
			expectError: false,
		},
		{
			name: "Invalid: Label key does not follow Kubernetes syntax",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.WorkerGroupSpecs[0].Labels = map[string]string{"invalid_key!": "value"}
				return s
			}(),
			expectError:  true,
			errorMessage: "invalid label key for worker-group group: 'invalid_key!', error: name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
		},
		{
			name: "Invalid: Label value does not follow Kubernetes syntax",
			spec: func() rayv1.RayClusterSpec {
				s := createSpec()
				s.HeadGroupSpec.Labels = map[string]string{"valid-key": "invalid/value"}
				return s
			}(),
			expectError:  true,
			errorMessage: "invalid label value for key 'valid-key' in Head group: 'invalid/value', error: a valid label must be an empty string or consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRayClusterSpec(&tt.spec, nil)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
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
			name: "RayJobDeletionPolicy feature gate must be enabled to use the DeletionStrategy feature",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteCluster),
					},
					OnFailure: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteCluster),
					},
				},
				ShutdownAfterJobFinishes: true,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "BackoffLimit is incompatible with InteractiveMode",
			spec: rayv1.RayJobSpec{
				BackoffLimit:   ptr.To[int32](1),
				SubmissionMode: rayv1.InteractiveMode,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "BackoffLimit is 0 and SubmissionMode is InteractiveMode",
			spec: rayv1.RayJobSpec{
				BackoffLimit:   ptr.To[int32](0),
				SubmissionMode: rayv1.InteractiveMode,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "BackoffLimit is nil and SubmissionMode is InteractiveMode",
			spec: rayv1.RayJobSpec{
				BackoffLimit:   nil,
				SubmissionMode: rayv1.InteractiveMode,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "ShutdownAfterJobFinishes is false and TTLSecondsAfterFinished is not zero",
			spec: rayv1.RayJobSpec{
				ShutdownAfterJobFinishes: false,
				TTLSecondsAfterFinished:  5,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "ShutdownAfterJobFinishes is true and TTLSecondsAfterFinished is not zero",
			spec: rayv1.RayJobSpec{
				ShutdownAfterJobFinishes: true,
				TTLSecondsAfterFinished:  5,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "ShutdownAfterJobFinishes is true and TTLSecondsAfterFinished is negative",
			spec: rayv1.RayJobSpec{
				ShutdownAfterJobFinishes: true,
				TTLSecondsAfterFinished:  -5,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "SidecarMode",
			spec: rayv1.RayJobSpec{
				SubmissionMode: rayv1.SidecarMode,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "SidecarMode doesn't support SubmitterPodTemplate",
			spec: rayv1.RayJobSpec{
				SubmissionMode: rayv1.SidecarMode,
				SubmitterPodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
			expectError: true,
		},
		{
			name: "SidecarMode doesn't support SubmitterConfig",
			spec: rayv1.RayJobSpec{
				SubmissionMode: rayv1.SidecarMode,
				SubmitterConfig: &rayv1.SubmitterConfig{
					BackoffLimit: ptr.To[int32](1),
				},
			},
			expectError: true,
		},
		{
			name: "SidecarMode RayCluster head pod should only be Never or unset",
			spec: rayv1.RayJobSpec{
				SubmissionMode: rayv1.SidecarMode,
				RayClusterSpec: &rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						Template: podTemplateSpec(nil, ptr.To(corev1.RestartPolicyAlways)),
					},
				},
			},
			expectError: true,
		},
		{
			name: "SidecarMode doesn't support ClusterSelector",
			spec: rayv1.RayJobSpec{
				SubmissionMode:  rayv1.SidecarMode,
				ClusterSelector: map[string]string{"ray.io/cluster": "ray-cluster"},
			},
			expectError: true,
		},
		{
			name: "failed to get cluster name in ClusterSelector map",
			spec: rayv1.RayJobSpec{
				ClusterSelector: map[string]string{},
			},
			expectError: true,
		},
		{
			name: "cluster name in ClusterSelector is empty",
			spec: rayv1.RayJobSpec{
				ClusterSelector: map[string]string{"ray.io/cluster": ""},
			},
			expectError: true,
		},
		{
			name: "cluster name in ClusterSelector is not empty",
			spec: rayv1.RayJobSpec{
				ClusterSelector: map[string]string{"ray.io/cluster": "ray-cluster"},
			},
			expectError: false,
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
		Template: podTemplateSpec(nil, nil),
	}

	tests := []struct {
		name        string
		spec        rayv1.RayJobSpec
		expectError bool
	}{
		// Legacy DeletionStrategy tests
		{
			name: "the ClusterSelector mode doesn't support DeletionStrategy=DeleteCluster",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteCluster),
					},
					OnFailure: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteCluster),
					},
				}, ClusterSelector: map[string]string{"key": "value"},
			},
			expectError: true,
		},
		{
			name: "the ClusterSelector mode doesn't support DeletionStrategy=DeleteWorkers",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteWorkers),
					},
					OnFailure: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteWorkers),
					},
				}, ClusterSelector: map[string]string{"key": "value"},
			},
			expectError: true,
		},
		{
			name: "DeletionStrategy=DeleteWorkers currently does not support RayCluster with autoscaling enabled",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteWorkers),
					},
					OnFailure: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteWorkers),
					},
				}, RayClusterSpec: &rayv1.RayClusterSpec{
					EnableInTreeAutoscaling: ptr.To(true),
					HeadGroupSpec:           headGroupSpecWithOneContainer,
				},
			},
			expectError: true,
		},
		{
			name: "valid RayJob with DeletionStrategy=DeleteCluster",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteCluster),
					},
					OnFailure: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteCluster),
					},
				}, ShutdownAfterJobFinishes: true,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "valid RayJob without DeletionStrategy",
			spec: rayv1.RayJobSpec{
				DeletionStrategy:         nil,
				ShutdownAfterJobFinishes: true,
				RayClusterSpec:           createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "shutdownAfterJobFinshes is set to 'true' while deletion policy is 'DeleteNone'",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteNone),
					},
					OnFailure: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteNone),
					},
				}, ShutdownAfterJobFinishes: true,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "OnSuccess unset",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnFailure: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteNone),
					},
				}, ShutdownAfterJobFinishes: true,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "OnSuccess.DeletionPolicyType unset",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnFailure: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteNone),
					},
				}, ShutdownAfterJobFinishes: true,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "OnFailure unset",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteNone),
					},
				}, ShutdownAfterJobFinishes: true,
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "OnFailure.DeletionPolicyType unset",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteNone),
					},
					OnFailure: &rayv1.DeletionPolicy{},
				}, ShutdownAfterJobFinishes: true,
				RayClusterSpec: createBasicRayClusterSpec(),
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
		// New Deletion Rules tests
		{
			name: "valid deletionRules",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
					},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: false,
		},
		{
			name: "deletionRules and ShutdownAfterJobFinishes both set",
			spec: rayv1.RayJobSpec{
				ShutdownAfterJobFinishes: true,
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
					},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "deletionRules and legacy onSuccess both set",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					OnSuccess: &rayv1.DeletionPolicy{
						Policy: ptr.To(rayv1.DeleteCluster),
					},
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
					},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "nil DeletionStrategy",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{},
				RayClusterSpec:   createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "empty DeletionStrategy",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "duplicate rule in deletionRules",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 20,
							},
						},
					},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "negative TTLSeconds in deletionRules",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: -10,
							},
						},
					},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "deletionRules with ClusterSelector and DeleteWorkers policy",
			spec: rayv1.RayJobSpec{
				ClusterSelector: map[string]string{"key": "value"},
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteWorkers,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "deletionRules with ClusterSelector and DeleteCluster policy",
			spec: rayv1.RayJobSpec{
				ClusterSelector: map[string]string{"key": "value"},
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteCluster,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "deletionRules with autoscaling and DeleteWorkers policy",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteWorkers,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
					},
				},
				RayClusterSpec: &rayv1.RayClusterSpec{
					EnableInTreeAutoscaling: ptr.To(true),
					HeadGroupSpec:           headGroupSpecWithOneContainer,
				},
			},
			expectError: true,
		},
		{
			name: "inconsistent TTLs in deletionRules (DeleteCluster < DeleteWorkers)",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteWorkers,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 20,
							},
						},
						{
							Policy: rayv1.DeleteCluster,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
					},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "inconsistent TTLs in deletionRules (DeleteSelf < DeleteCluster)",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteCluster,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 20,
							},
						},
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
					},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: true,
		},
		{
			name: "valid complex deletionRules",
			spec: rayv1.RayJobSpec{
				DeletionStrategy: &rayv1.DeletionStrategy{
					DeletionRules: []rayv1.DeletionRule{
						{
							Policy: rayv1.DeleteWorkers,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 10,
							},
						},
						{
							Policy: rayv1.DeleteCluster,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 20,
							},
						},
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusSucceeded,
								TTLSeconds: 30,
							},
						},
						{
							Policy: rayv1.DeleteSelf,
							Condition: rayv1.DeletionCondition{
								JobStatus:  rayv1.JobStatusFailed,
								TTLSeconds: 0,
							},
						},
					},
				},
				RayClusterSpec: createBasicRayClusterSpec(),
			},
			expectError: false,
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
		{
			name: "Spec.RayClusterDeletionDelaySeconds is negative",
			spec: rayv1.RayServiceSpec{
				RayClusterSpec:                 *createBasicRayClusterSpec(),
				RayClusterDeletionDelaySeconds: ptr.To[int32](-1),
			},
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

	// Test valid initializing timeout annotations
	err = ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: "valid-service",
		Annotations: map[string]string{
			RayServiceInitializingTimeoutAnnotation: "5m",
		},
	})
	require.NoError(t, err)

	err = ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: "valid-service",
		Annotations: map[string]string{
			RayServiceInitializingTimeoutAnnotation: "300",
		},
	})
	require.NoError(t, err)

	// Test invalid initializing timeout annotations
	err = ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: "invalid-service",
		Annotations: map[string]string{
			RayServiceInitializingTimeoutAnnotation: "0",
		},
	})
	require.ErrorContains(t, err, "must be a positive")

	err = ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: "invalid-service",
		Annotations: map[string]string{
			RayServiceInitializingTimeoutAnnotation: "-100",
		},
	})
	require.ErrorContains(t, err, "must be a positive")

	err = ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: "invalid-service",
		Annotations: map[string]string{
			RayServiceInitializingTimeoutAnnotation: "-5m",
		},
	})
	require.ErrorContains(t, err, "must be a positive duration")

	err = ValidateRayServiceMetadata(metav1.ObjectMeta{
		Name: "invalid-service",
		Annotations: map[string]string{
			RayServiceInitializingTimeoutAnnotation: "invalid",
		},
	})
	require.ErrorContains(t, err, "invalid format")
}

func createBasicRayClusterSpec() *rayv1.RayClusterSpec {
	return &rayv1.RayClusterSpec{
		HeadGroupSpec: rayv1.HeadGroupSpec{
			Template: podTemplateSpec(nil, nil),
		},
	}
}

func TestValidateClusterUpgradeOptions(t *testing.T) {
	tests := []struct {
		maxSurgePercent   *int32
		stepSizePercent   *int32
		intervalSeconds   *int32
		name              string
		gatewayClassName  string
		spec              rayv1.RayServiceSpec
		enableAutoscaling bool
		expectError       bool
	}{
		{
			name:              "valid config",
			maxSurgePercent:   ptr.To(int32(50)),
			stepSizePercent:   ptr.To(int32(50)),
			intervalSeconds:   ptr.To(int32(10)),
			gatewayClassName:  "istio",
			enableAutoscaling: true,
			expectError:       false,
		},
		{
			name:              "missing autoscaler",
			maxSurgePercent:   ptr.To(int32(50)),
			stepSizePercent:   ptr.To(int32(50)),
			intervalSeconds:   ptr.To(int32(10)),
			gatewayClassName:  "istio",
			enableAutoscaling: false,
			expectError:       true,
		},
		{
			name:              "missing options",
			enableAutoscaling: true,
			expectError:       true,
		},
		{
			name:              "invalid MaxSurgePercent",
			maxSurgePercent:   ptr.To(int32(200)),
			stepSizePercent:   ptr.To(int32(50)),
			intervalSeconds:   ptr.To(int32(10)),
			gatewayClassName:  "istio",
			enableAutoscaling: true,
			expectError:       true,
		},
		{
			name:              "missing StepSizePercent",
			maxSurgePercent:   ptr.To(int32(50)),
			intervalSeconds:   ptr.To(int32(10)),
			gatewayClassName:  "istio",
			enableAutoscaling: true,
			expectError:       true,
		},
		{
			name:              "invalid IntervalSeconds",
			maxSurgePercent:   ptr.To(int32(50)),
			stepSizePercent:   ptr.To(int32(50)),
			intervalSeconds:   ptr.To(int32(0)),
			gatewayClassName:  "istio",
			enableAutoscaling: true,
			expectError:       true,
		},
		{
			name:              "missing GatewayClassName",
			maxSurgePercent:   ptr.To(int32(50)),
			stepSizePercent:   ptr.To(int32(50)),
			intervalSeconds:   ptr.To(int32(10)),
			enableAutoscaling: true,
			expectError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var upgradeStrategy *rayv1.RayServiceUpgradeStrategy
			if tt.maxSurgePercent != nil || tt.stepSizePercent != nil || tt.intervalSeconds != nil || tt.gatewayClassName != "" {
				upgradeStrategy = &rayv1.RayServiceUpgradeStrategy{
					Type: ptr.To(rayv1.NewClusterWithIncrementalUpgrade),
					ClusterUpgradeOptions: &rayv1.ClusterUpgradeOptions{
						MaxSurgePercent:  tt.maxSurgePercent,
						StepSizePercent:  tt.stepSizePercent,
						IntervalSeconds:  tt.intervalSeconds,
						GatewayClassName: tt.gatewayClassName,
					},
				}
			} else if tt.expectError {
				upgradeStrategy = &rayv1.RayServiceUpgradeStrategy{
					Type: ptr.To(rayv1.NewClusterWithIncrementalUpgrade),
				}
			}

			rayClusterSpec := *createBasicRayClusterSpec()
			rayClusterSpec.EnableInTreeAutoscaling = ptr.To(tt.enableAutoscaling)

			rayService := &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec:  rayClusterSpec,
					UpgradeStrategy: upgradeStrategy,
				},
			}

			err := ValidateClusterUpgradeOptions(rayService)
			if tt.expectError {
				require.Error(t, err, tt.name)
			} else {
				require.NoError(t, err, tt.name)
			}
		})
	}
}

func TestValidateWorkerGroupIdleTimeout(t *testing.T) {
	tests := map[string]struct {
		expectedErr string
		spec        rayv1.RayClusterSpec
	}{
		"should accept worker group with valid idleTimeoutSeconds": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV2),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName:          "worker-group-1",
						Template:           podTemplateSpec(nil, nil),
						IdleTimeoutSeconds: ptr.To(int32(60)),
						MinReplicas:        ptr.To(int32(0)),
						MaxReplicas:        ptr.To(int32(10)),
					},
				},
			},
			expectedErr: "",
		},
		"should reject negative idleTimeoutSeconds": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV2),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName:          "worker-group-1",
						Template:           podTemplateSpec(nil, nil),
						IdleTimeoutSeconds: ptr.To(int32(-10)),
						MinReplicas:        ptr.To(int32(0)),
						MaxReplicas:        ptr.To(int32(10)),
					},
				},
			},
			expectedErr: "idleTimeoutSeconds must be non-negative, got -10",
		},
		"should accept zero idleTimeoutSeconds": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV2),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName:          "worker-group-1",
						Template:           podTemplateSpec(nil, nil),
						IdleTimeoutSeconds: ptr.To(int32(0)),
						MinReplicas:        ptr.To(int32(0)),
						MaxReplicas:        ptr.To(int32(10)),
					},
				},
			},
			expectedErr: "",
		},
		"should reject idleTimeoutSeconds when autoscaler version is not v2": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV1),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName:          "worker-group-1",
						Template:           podTemplateSpec(nil, nil),
						IdleTimeoutSeconds: ptr.To(int32(60)),
						MinReplicas:        ptr.To(int32(0)),
						MaxReplicas:        ptr.To(int32(10)),
					},
				},
			},
			expectedErr: "worker group worker-group-1 has idleTimeoutSeconds set, but autoscaler version is not v2. Please set .spec.autoscalerOptions.version to v2",
		},
		"should reject idleTimeoutSeconds when autoscaler version is not set": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName:          "worker-group-1",
						Template:           podTemplateSpec(nil, nil),
						IdleTimeoutSeconds: ptr.To(int32(60)),
						MinReplicas:        ptr.To(int32(0)),
						MaxReplicas:        ptr.To(int32(10)),
					},
				},
			},
			expectedErr: "worker group worker-group-1 has idleTimeoutSeconds set, but autoscaler version is not v2. Please set .spec.autoscalerOptions.version to v2",
		},
		"should reject idleTimeoutSeconds when AutoscalerOptions is nil": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions:       nil,
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName:          "worker-group-1",
						Template:           podTemplateSpec(nil, nil),
						IdleTimeoutSeconds: ptr.To(int32(60)),
						MinReplicas:        ptr.To(int32(0)),
						MaxReplicas:        ptr.To(int32(10)),
					},
				},
			},
			expectedErr: "worker group worker-group-1 has idleTimeoutSeconds set, but autoscaler version is not v2. Please set .spec.autoscalerOptions.version to v2",
		},
		"should accept worker group without idleTimeoutSeconds and without autoscaler v2": {
			spec: rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
				AutoscalerOptions: &rayv1.AutoscalerOptions{
					Version: ptr.To(rayv1.AutoscalerVersionV1),
				},
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: podTemplateSpec(nil, nil),
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName:   "worker-group-1",
						Template:    podTemplateSpec(nil, nil),
						MinReplicas: ptr.To(int32(0)),
						MaxReplicas: ptr.To(int32(10)),
					},
				},
			},
			expectedErr: "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateRayClusterSpec(&tc.spec, nil)
			if tc.expectedErr != "" {
				require.Error(t, err)
				require.EqualError(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
