package utils

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/assert"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestValidateRayJobSpec(t *testing.T) {
	err := ValidateRayJobSpec(&rayv1.RayJob{})
	assert.ErrorContains(t, err, "one of RayClusterSpec or ClusterSelector must be set")

	err = ValidateRayJobSpec(&rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			Suspend:                  true,
			ShutdownAfterJobFinishes: false,
		},
	})
	assert.ErrorContains(t, err, "a RayJob with shutdownAfterJobFinishes set to false is not allowed to be suspended")

	err = ValidateRayJobSpec(&rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			Suspend:                  true,
			ShutdownAfterJobFinishes: true,
			RayClusterSpec:           &rayv1.RayClusterSpec{},
		},
	})
	assert.NoError(t, err)

	err = ValidateRayJobSpec(&rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			Suspend:                  true,
			ShutdownAfterJobFinishes: true,
			ClusterSelector: map[string]string{
				"key": "value",
			},
		},
	})
	assert.ErrorContains(t, err, "the ClusterSelector mode doesn't support the suspend operation")

	err = ValidateRayJobSpec(&rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			RuntimeEnvYAML: "invalid_yaml_str",
			RayClusterSpec: &rayv1.RayClusterSpec{},
		},
	})
	assert.ErrorContains(t, err, "failed to unmarshal RuntimeEnvYAML")

	err = ValidateRayJobSpec(&rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			BackoffLimit:   ptr.To[int32](-1),
			RayClusterSpec: &rayv1.RayClusterSpec{},
		},
	})
	assert.ErrorContains(t, err, "backoffLimit must be a positive integer")

	err = ValidateRayJobSpec(&rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			DeletionPolicy:           ptr.To(rayv1.DeleteClusterDeletionPolicy),
			ShutdownAfterJobFinishes: true,
			RayClusterSpec:           &rayv1.RayClusterSpec{},
		},
	})
	assert.NoError(t, err)

	err = ValidateRayJobSpec(&rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			DeletionPolicy:           nil,
			ShutdownAfterJobFinishes: true,
			RayClusterSpec:           &rayv1.RayClusterSpec{},
		},
	})
	assert.NoError(t, err)

	err = ValidateRayJobSpec(&rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			DeletionPolicy:           ptr.To(rayv1.DeleteNoneDeletionPolicy),
			ShutdownAfterJobFinishes: true,
			RayClusterSpec:           &rayv1.RayClusterSpec{},
		},
	})
	assert.ErrorContains(t, err, "shutdownAfterJobFinshes is set to 'true' while deletion policy is 'DeleteNone'")
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
				t.Errorf("validateRayClusterStatus() error = %v, wantErr %v", err, tt.expectError)
			}
		})
	}
}
