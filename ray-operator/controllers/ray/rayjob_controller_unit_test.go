package ray

import (
	"context"
	"testing"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	utils "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateK8sJobIfNeed(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = batchv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	rayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-raycluster",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Image: "rayproject/ray",
							},
						},
					},
				},
			},
		},
	}

	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rayjob",
			Namespace: "default",
		},
	}

	k8sJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rayjob",
			Namespace: "default",
		},
	}

	// Test 1: Return the existing k8s job if it already exists
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(k8sJob, rayCluster, rayJob).Build()
	ctx := context.TODO()

	rayJobReconciler := &RayJobReconciler{
		Client:   fakeClient,
		Log:      ctrl.Log.WithName("controllers").WithName("RayJob"),
		Scheme:   newScheme,
		Recorder: &record.FakeRecorder{},
	}

	err := rayJobReconciler.createK8sJobIfNeed(ctx, rayJob, rayCluster)
	assert.NoError(t, err)

	// Test 2: Create a new k8s job if it does not already exist
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(rayCluster, rayJob).Build()
	rayJobReconciler.Client = fakeClient

	err = rayJobReconciler.createK8sJobIfNeed(ctx, rayJob, rayCluster)
	assert.NoError(t, err)
}

func TestGetSubmitterTemplate(t *testing.T) {
	// RayJob instance with user-provided submitter pod template.
	rayJobInstanceWithTemplate := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			Entrypoint: "echo hello world",
			SubmitterPodTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Command: []string{"user-command"},
						},
					},
				},
			},
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "test-url",
		},
	}

	// RayJob instance without user-provided submitter pod template.
	// In this case we should use the image of the Ray Head, so specify the image so we can test it.
	rayJobInstanceWithoutTemplate := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			Entrypoint: "echo hello world",
			RayClusterSpec: &rayv1.RayClusterSpec{
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image: "rayproject/ray:custom-version",
								},
							},
						},
					},
				},
			},
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "test-url",
		},
	}
	rayClusterInstance := &rayv1.RayCluster{
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Image: "rayproject/ray:custom-version",
							},
						},
					},
				},
			},
		},
	}

	r := &RayJobReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("RayJob"),
	}

	// Test 1: User provided template with command
	submitterTemplate, err := r.getSubmitterTemplate(rayJobInstanceWithTemplate, nil)
	assert.NoError(t, err)
	assert.Equal(t, "user-command", submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command[0])

	// Test 2: User provided template without command
	rayJobInstanceWithTemplate.Spec.SubmitterPodTemplate.Spec.Containers[utils.RayContainerIndex].Command = []string{}
	submitterTemplate, err = r.getSubmitterTemplate(rayJobInstanceWithTemplate, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{"ray", "job", "submit", "--address", "http://test-url", "--", "echo", "hello", "world"}, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command)

	// Test 3: User did not provide template, should use the image of the Ray Head
	submitterTemplate, err = r.getSubmitterTemplate(rayJobInstanceWithoutTemplate, rayClusterInstance)
	assert.NoError(t, err)
	assert.Equal(t, []string{"ray", "job", "submit", "--address", "http://test-url", "--", "echo", "hello", "world"}, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command)
	assert.Equal(t, "rayproject/ray:custom-version", submitterTemplate.Spec.Containers[utils.RayContainerIndex].Image)

	// Test 4: Check default PYTHONUNBUFFERED setting
	submitterTemplate, err = r.getSubmitterTemplate(rayJobInstanceWithoutTemplate, rayClusterInstance)
	assert.NoError(t, err)
	found := false
	for _, envVar := range submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env {
		if envVar.Name == PythonUnbufferedEnvVarName {
			assert.Equal(t, "1", envVar.Value)
			found = true
		}
	}
	assert.True(t, found)
}

func TestUpdateStatusToSuspendingIfNeeded(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	tests := map[string]struct {
		suspend               bool
		status                rayv1.JobDeploymentStatus
		isClusterSelectorMode bool
		expectedShouldUpdate  bool
	}{
		// When Autoscaler is enabled, the random Pod deletion is controleld by the feature flag `ENABLE_RANDOM_POD_DELETE`.
		"Suspend is false": {
			suspend:               false,
			status:                rayv1.JobDeploymentStatusInitializing,
			isClusterSelectorMode: false,
			expectedShouldUpdate:  false,
		},
		"Suspend is true, but the RayJob is in ClusterSelector mode": {
			suspend:               true,
			status:                rayv1.JobDeploymentStatusInitializing,
			isClusterSelectorMode: true,
			expectedShouldUpdate:  false,
		},
		"Suspend is true, but the status is not allowed to transition to suspending": {
			suspend:               true,
			status:                rayv1.JobDeploymentStatusComplete,
			isClusterSelectorMode: false,
			expectedShouldUpdate:  false,
		},
		"Suspend is true, and the status is allowed to transition to suspending": {
			suspend:               true,
			status:                rayv1.JobDeploymentStatusInitializing,
			isClusterSelectorMode: false,
			expectedShouldUpdate:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			name := "test-rayjob"
			namespace := "default"
			rayJob := &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: rayv1.RayJobSpec{
					Suspend: tc.suspend,
				},
				Status: rayv1.RayJobStatus{
					JobDeploymentStatus: tc.status,
				},
			}

			if tc.isClusterSelectorMode {
				rayJob.Spec.ClusterSelector = map[string]string{
					"key": "value",
				}
			}

			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(newScheme).
				WithRuntimeObjects(rayJob).
				WithStatusSubresource(rayJob).Build()
			ctx := context.Background()

			// Initialize a new RayClusterReconciler.
			testRayJobReconciler := &RayJobReconciler{
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   newScheme,
				Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
			}
			shouldUpdate := testRayJobReconciler.updateStatusToSuspendingIfNeeded(ctx, rayJob)
			assert.Equal(t, tc.expectedShouldUpdate, shouldUpdate)

			if tc.expectedShouldUpdate {
				assert.Equal(t, rayv1.JobDeploymentStatusSuspending, rayJob.Status.JobDeploymentStatus)
			} else {
				assert.Equal(t, tc.status, rayJob.Status.JobDeploymentStatus)
			}
		})
	}
}
