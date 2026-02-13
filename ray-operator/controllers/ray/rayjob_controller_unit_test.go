package ray

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics/mocks"
	utils "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
)

func TestCreateRayJobSubmitterIfNeed(t *testing.T) {
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
		Scheme:   newScheme,
		Recorder: &record.FakeRecorder{},
	}

	err := rayJobReconciler.createK8sJobIfNeed(ctx, rayJob, rayCluster)
	require.NoError(t, err)

	// Test 2: Create a new k8s job if it does not already exist
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(rayCluster, rayJob).Build()
	rayJobReconciler.Client = fakeClient

	err = rayJobReconciler.createK8sJobIfNeed(ctx, rayJob, rayCluster)
	require.NoError(t, err)

	err = fakeClient.Get(ctx, types.NamespacedName{
		Namespace: k8sJob.Namespace,
		Name:      k8sJob.Name,
	}, k8sJob, nil)
	require.NoError(t, err)

	assert.Equal(t, k8sJob.Labels[utils.RayOriginatedFromCRNameLabelKey], rayJob.Name)
	assert.Equal(t, k8sJob.Labels[utils.RayOriginatedFromCRDLabelKey], utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD))
}

func TestGetSubmitterTemplate(t *testing.T) {
	// RayJob instance with user-provided submitter pod template.
	rayJobInstanceWithTemplate := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			Entrypoint: "echo no quote 'single quote' \"double quote\"",
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
			JobId:        "test-job-id",
		},
	}

	// RayJob instance without user-provided submitter pod template.
	// In this case we should use the image of the Ray Head, so specify the image so we can test it.
	rayJobInstanceWithoutTemplate := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			Entrypoint: "echo no quote 'single quote' \"double quote\"",
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
			JobId:        "test-job-id",
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

	// Test 1: User provided template with command
	submitterTemplate, err := getSubmitterTemplate(rayJobInstanceWithTemplate, rayClusterInstance)
	require.NoError(t, err)
	assert.Equal(t, "user-command", submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command[0])

	// Test 2: User provided template without command
	rayJobInstanceWithTemplate.Spec.SubmitterPodTemplate.Spec.Containers[utils.RayContainerIndex].Command = []string{}
	submitterTemplate, err = getSubmitterTemplate(rayJobInstanceWithTemplate, rayClusterInstance)
	require.NoError(t, err)
	assert.Equal(t, []string{"/bin/bash", "-ce", "--"}, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command)
	assert.Equal(t, []string{"if ! ray job status --address http://test-url test-job-id >/dev/null 2>&1 ; then ray job submit --address http://test-url --no-wait --submission-id test-job-id -- echo no quote 'single quote' \"double quote\" ; fi ; ray job logs --address http://test-url --follow test-job-id"}, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Args)

	// Test 3: User did not provide template, should use the image of the Ray Head
	submitterTemplate, err = getSubmitterTemplate(rayJobInstanceWithoutTemplate, rayClusterInstance)
	require.NoError(t, err)
	assert.Equal(t, []string{"/bin/bash", "-ce", "--"}, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command)
	assert.Equal(t, []string{"if ! ray job status --address http://test-url test-job-id >/dev/null 2>&1 ; then ray job submit --address http://test-url --no-wait --submission-id test-job-id -- echo no quote 'single quote' \"double quote\" ; fi ; ray job logs --address http://test-url --follow test-job-id"}, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Args)
	assert.Equal(t, "rayproject/ray:custom-version", submitterTemplate.Spec.Containers[utils.RayContainerIndex].Image)

	// Test 4: Check default PYTHONUNBUFFERED setting
	submitterTemplate, err = getSubmitterTemplate(rayJobInstanceWithoutTemplate, rayClusterInstance)
	require.NoError(t, err)

	envVar, found := utils.EnvVarByName(PythonUnbufferedEnvVarName, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env)
	assert.True(t, found)
	assert.Equal(t, "1", envVar.Value)

	// Test 5: Check default RAY_DASHBOARD_ADDRESS env var
	submitterTemplate, err = getSubmitterTemplate(rayJobInstanceWithTemplate, rayClusterInstance)
	require.NoError(t, err)

	envVar, found = utils.EnvVarByName(utils.RAY_DASHBOARD_ADDRESS, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env)
	assert.True(t, found)
	assert.Equal(t, "test-url", envVar.Value)

	// Test 6: Check default RAY_JOB_SUBMISSION_ID env var
	envVar, found = utils.EnvVarByName(utils.RAY_JOB_SUBMISSION_ID, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Env)
	assert.True(t, found)
	assert.Equal(t, "test-job-id", envVar.Value)
}

func TestGetSubmitterContainer(t *testing.T) {
	// Helper to create a basic RayClusterSpec with a head container
	basicRayClusterSpec := func() *rayv1.RayClusterSpec {
		return &rayv1.RayClusterSpec{
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
		}
	}

	rayClusterInstance := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-raycluster",
			Namespace: "default",
		},
		Spec: *basicRayClusterSpec(),
	}

	t.Run("Default container without SubmitterContainerTemplate", func(t *testing.T) {
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint:                 "python script.py",
				SubmitterContainerTemplate: nil,
				RayClusterSpec:             basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// Should use default values
		assert.Equal(t, utils.SubmitterContainerName, container.Name)
		assert.Equal(t, "rayproject/ray:custom-version", container.Image)
		assert.NotEmpty(t, container.Resources.Limits)
		assert.NotEmpty(t, container.Resources.Requests)
	})

	t.Run("Custom container with SubmitterContainerTemplate", func(t *testing.T) {
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python script.py",
				SubmitterContainerTemplate: &corev1.Container{
					Image: "custom/image:v1",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "scripts",
							MountPath: "/scripts",
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "MY_VAR",
							Value: "my_value",
						},
					},
				},
				RayClusterSpec: basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// Container name should be overwritten
		assert.Equal(t, utils.SubmitterContainerName, container.Name)
		// Custom image should be preserved
		assert.Equal(t, "custom/image:v1", container.Image)
		// Volume mounts should be preserved
		assert.Len(t, container.VolumeMounts, 1)
		assert.Equal(t, "scripts", container.VolumeMounts[0].Name)
		assert.Equal(t, "/scripts", container.VolumeMounts[0].MountPath)
		// User env vars should be present (before controller env vars)
		envVar, found := utils.EnvVarByName("MY_VAR", container.Env)
		assert.True(t, found)
		assert.Equal(t, "my_value", envVar.Value)
		// Controller env vars should also be present
		envVar, found = utils.EnvVarByName(utils.RAY_DASHBOARD_ADDRESS, container.Env)
		assert.True(t, found)
		assert.Equal(t, "test-url", envVar.Value)
	})

	t.Run("Image inheritance when not specified in template", func(t *testing.T) {
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python script.py",
				SubmitterContainerTemplate: &corev1.Container{
					// Image not specified - should inherit from head container
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: "/data",
						},
					},
				},
				RayClusterSpec: basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// Should inherit image from head container
		assert.Equal(t, "rayproject/ray:custom-version", container.Image)
		// Volume mounts should still be preserved
		assert.Len(t, container.VolumeMounts, 1)
		assert.Equal(t, "data", container.VolumeMounts[0].Name)
	})

	t.Run("Container name is always overwritten", func(t *testing.T) {
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python script.py",
				SubmitterContainerTemplate: &corev1.Container{
					Name:  "user-specified-name", // Should be overwritten
					Image: "custom/image:v1",
				},
				RayClusterSpec: basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// Name should be overwritten to the standard submitter container name
		assert.Equal(t, utils.SubmitterContainerName, container.Name)
	})

	t.Run("Command and Args are always overwritten in SidecarMode", func(t *testing.T) {
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python script.py",
				SubmitterContainerTemplate: &corev1.Container{
					Image:   "custom/image:v1",
					Command: []string{"user-command"}, // Should be overwritten
					Args:    []string{"user-args"},    // Should be overwritten
				},
				RayClusterSpec: basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// Command should be overwritten to the controller's command
		assert.Equal(t, []string{"/bin/bash", "-ce", "--"}, container.Command)
		// Args should contain the job submission command
		assert.Len(t, container.Args, 1)
		assert.Contains(t, container.Args[0], "ray job submit")
	})

	t.Run("Default resources when not specified in template", func(t *testing.T) {
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python script.py",
				SubmitterContainerTemplate: &corev1.Container{
					Image: "custom/image:v1",
					// Resources not specified - should use defaults
				},
				RayClusterSpec: basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// Should have default resources
		assert.NotEmpty(t, container.Resources.Limits)
		assert.NotEmpty(t, container.Resources.Requests)
	})

	t.Run("Custom resources are preserved", func(t *testing.T) {
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python script.py",
				SubmitterContainerTemplate: &corev1.Container{
					Image: "custom/image:v1",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
				RayClusterSpec: basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// Custom resources should be preserved
		assert.NotEmpty(t, container.Resources.Limits)
		assert.Equal(t, resource.MustParse("2"), container.Resources.Limits[corev1.ResourceCPU])
	})

	t.Run("Security context is preserved", func(t *testing.T) {
		runAsUser := int64(1000)
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python script.py",
				SubmitterContainerTemplate: &corev1.Container{
					Image: "custom/image:v1",
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:    &runAsUser,
						RunAsNonRoot: pointer.Bool(true),
					},
				},
				RayClusterSpec: basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// Security context should be preserved
		assert.NotNil(t, container.SecurityContext)
		assert.Equal(t, int64(1000), *container.SecurityContext.RunAsUser)
		assert.True(t, *container.SecurityContext.RunAsNonRoot)
	})

	t.Run("EnvFrom is preserved", func(t *testing.T) {
		rayJobInstance := &rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python script.py",
				SubmitterContainerTemplate: &corev1.Container{
					Image: "custom/image:v1",
					EnvFrom: []corev1.EnvFromSource{
						{
							ConfigMapRef: &corev1.ConfigMapEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "my-config",
								},
							},
						},
					},
				},
				RayClusterSpec: basicRayClusterSpec(),
			},
			Status: rayv1.RayJobStatus{
				DashboardURL: "test-url",
				JobId:        "test-job-id",
			},
		}

		container, err := getSubmitterContainer(rayJobInstance, rayClusterInstance)
		require.NoError(t, err)

		// EnvFrom should be preserved
		assert.Len(t, container.EnvFrom, 1)
		assert.Equal(t, "my-config", container.EnvFrom[0].ConfigMapRef.Name)
	})
}

func TestUpdateStatusToSuspendingIfNeeded(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	tests := []struct {
		name                 string
		status               rayv1.JobDeploymentStatus
		suspend              bool
		expectedShouldUpdate bool
	}{
		// When Autoscaler is enabled, the random Pod deletion is controleld by the feature flag `ENABLE_RANDOM_POD_DELETE`.
		{
			name:                 "Suspend is false",
			suspend:              false,
			status:               rayv1.JobDeploymentStatusInitializing,
			expectedShouldUpdate: false,
		},
		{
			name:                 "Suspend is true, but the status is not allowed to transition to suspending",
			suspend:              true,
			status:               rayv1.JobDeploymentStatusComplete,
			expectedShouldUpdate: false,
		},
		{
			name:                 "Suspend is true, and the status is allowed to transition to suspending",
			suspend:              true,
			status:               rayv1.JobDeploymentStatusInitializing,
			expectedShouldUpdate: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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

			ctx := context.Background()
			shouldUpdate := updateStatusToSuspendingIfNeeded(ctx, rayJob)
			assert.Equal(t, tc.expectedShouldUpdate, shouldUpdate)

			if tc.expectedShouldUpdate {
				assert.Equal(t, rayv1.JobDeploymentStatusSuspending, rayJob.Status.JobDeploymentStatus)
			} else {
				assert.Equal(t, tc.status, rayJob.Status.JobDeploymentStatus)
			}
		})
	}
}

func TestUpdateRayJobStatus(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)

	rayJobTemplate := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rayjob",
			Namespace: "default",
		},
		Status: rayv1.RayJobStatus{
			JobDeploymentStatus: rayv1.JobDeploymentStatusRunning,
			JobStatus:           rayv1.JobStatusRunning,
			Message:             "old message",
		},
	}
	newMessage := "new message"

	tests := []struct {
		name                         string
		isJobDeploymentStatusChanged bool
	}{
		{
			name:                         "JobDeploymentStatus is not changed",
			isJobDeploymentStatusChanged: false,
		},
		{
			name:                         "JobDeploymentStatus is changed",
			isJobDeploymentStatusChanged: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldRayJob := rayJobTemplate.DeepCopy()

			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(newScheme).
				WithRuntimeObjects(oldRayJob).
				WithStatusSubresource(oldRayJob).Build()
			ctx := context.Background()

			newRayJob := &rayv1.RayJob{}
			err := fakeClient.Get(ctx, types.NamespacedName{Namespace: oldRayJob.Namespace, Name: oldRayJob.Name}, newRayJob)
			require.NoError(t, err)

			// Update the status
			newRayJob.Status.Message = newMessage
			if tc.isJobDeploymentStatusChanged {
				newRayJob.Status.JobDeploymentStatus = rayv1.JobDeploymentStatusSuspending
			}

			// Initialize a new RayClusterReconciler.
			testRayJobReconciler := &RayJobReconciler{
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   newScheme,
			}

			err = testRayJobReconciler.updateRayJobStatus(ctx, oldRayJob, newRayJob)
			require.NoError(t, err)

			err = fakeClient.Get(ctx, types.NamespacedName{Namespace: newRayJob.Namespace, Name: newRayJob.Name}, newRayJob)
			require.NoError(t, err)
			assert.Equal(t, newRayJob.Status.Message == newMessage, tc.isJobDeploymentStatusChanged)
		})
	}
}

func TestFailedToCreateRayJobSubmitterEvent(t *testing.T) {
	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rayjob",
			Namespace: "default",
		},
	}

	submitterTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-submit-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "ray-submit",
					Image: "rayproject/ray:latest",
				},
			},
		},
	}

	fakeClient := clientFake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
			return errors.New("random")
		},
	}).WithScheme(scheme.Scheme).Build()

	recorder := record.NewFakeRecorder(100)

	reconciler := &RayJobReconciler{
		Client:   fakeClient,
		Recorder: recorder,
		Scheme:   scheme.Scheme,
	}

	err := reconciler.createNewK8sJob(context.Background(), rayJob, submitterTemplate)

	require.Error(t, err, "Expected error due to simulated job creation failure")

	var foundFailureEvent bool
	events := []string{}
	for len(recorder.Events) > 0 {
		event := <-recorder.Events
		if strings.Contains(event, "Failed to create new Kubernetes Job") {
			foundFailureEvent = true
			break
		}
		events = append(events, event)
	}

	assert.Truef(t, foundFailureEvent, "Expected event to be generated for job creation failure, got events: %s", strings.Join(events, "\n"))
}

func TestFailedCreateRayClusterEvent(t *testing.T) {
	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rayjob",
			Namespace: "default",
		},
		Spec: rayv1.RayJobSpec{
			RayClusterSpec: &rayv1.RayClusterSpec{},
		},
	}

	fakeClient := clientFake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
			return errors.New("random")
		},
	}).WithScheme(scheme.Scheme).Build()

	recorder := record.NewFakeRecorder(100)

	reconciler := &RayJobReconciler{
		Client:   fakeClient,
		Recorder: recorder,
		Scheme:   scheme.Scheme,
	}

	_, err := reconciler.getOrCreateRayClusterInstance(context.Background(), rayJob)

	require.Error(t, err, "Expected error due to cluster creation failure")

	var foundFailureEvent bool
	events := []string{}
	for len(recorder.Events) > 0 {
		event := <-recorder.Events
		if strings.Contains(event, "Failed to create RayCluster") {
			foundFailureEvent = true
			break
		}
		events = append(events, event)
	}

	assert.Truef(t, foundFailureEvent, "Expected event to be generated for cluster creation failure, got events: %s", strings.Join(events, "\n"))
}

func TestFailedDeleteRayJobSubmitterEvent(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(newScheme)

	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rayjob",
			Namespace: "default",
		},
	}
	submitter := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rayjob",
			Namespace: "default",
		},
	}

	fakeClient := clientFake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
			return errors.New("random")
		},
	}).WithScheme(newScheme).WithRuntimeObjects(submitter).Build()

	recorder := record.NewFakeRecorder(100)

	reconciler := &RayJobReconciler{
		Client:   fakeClient,
		Recorder: recorder,
		Scheme:   scheme.Scheme,
	}

	_, err := reconciler.deleteSubmitterJob(context.Background(), rayJob)

	require.Error(t, err, "Expected error due to job deletion failure")

	var foundFailureEvent bool
	events := []string{}
	for len(recorder.Events) > 0 {
		event := <-recorder.Events
		if strings.Contains(event, "Failed to delete submitter K8s Job") {
			foundFailureEvent = true
			break
		}
		events = append(events, event)
	}

	assert.Truef(t, foundFailureEvent, "Expected event to be generated for cluster deletion failure, got events: %s", strings.Join(events, "\n"))
}

func TestFailedDeleteRayClusterEvent(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)

	rayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-raycluster",
			Namespace: "default",
		},
	}

	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rayjob",
			Namespace: "default",
		},
		Status: rayv1.RayJobStatus{
			RayClusterName: "test-raycluster",
		},
	}

	fakeClient := clientFake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
			return errors.New("random")
		},
	}).WithScheme(newScheme).WithRuntimeObjects(rayCluster).Build()

	recorder := record.NewFakeRecorder(100)

	reconciler := &RayJobReconciler{
		Client:   fakeClient,
		Recorder: recorder,
		Scheme:   scheme.Scheme,
	}

	_, err := reconciler.deleteClusterResources(context.Background(), rayJob)

	require.Error(t, err, "Expected error due to cluster deletion failure")

	var foundFailureEvent bool
	events := []string{}
	for len(recorder.Events) > 0 {
		event := <-recorder.Events
		if strings.Contains(event, "Failed to delete cluster") {
			foundFailureEvent = true
			break
		}
		events = append(events, event)
	}

	assert.Truef(t, foundFailureEvent, "Expected event to be generated for cluster deletion failure, got events: %s", strings.Join(events, "\n"))
}

func TestEmitRayJobExecutionDuration(t *testing.T) {
	rayJobName := "test-job"
	rayJobNamespace := "default"
	rayJobUID := types.UID("test-job-uid")
	mockTime := time.Now().Add(-60 * time.Second)

	//nolint:govet // disable govet to keep the order of the struct fields
	tests := []struct {
		name                        string
		originalRayJobStatus        rayv1.RayJobStatus
		rayJobStatus                rayv1.RayJobStatus
		expectMetricsCall           bool
		expectedJobDeploymentStatus rayv1.JobDeploymentStatus
		expectedRetryCount          int
		expectedDuration            float64
	}{
		{
			name: "non-terminal to complete state should emit metrics",
			originalRayJobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusRunning,
			},
			rayJobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusComplete,
				StartTime:           &metav1.Time{Time: mockTime},
			},
			expectMetricsCall:           true,
			expectedJobDeploymentStatus: rayv1.JobDeploymentStatusComplete,
			expectedRetryCount:          0,
			expectedDuration:            60.0,
		},
		{
			name: "non-terminal to failed state should emit metrics",
			originalRayJobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusRunning,
			},
			rayJobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusFailed,
				StartTime:           &metav1.Time{Time: mockTime},
			},
			expectMetricsCall:           true,
			expectedJobDeploymentStatus: rayv1.JobDeploymentStatusFailed,
			expectedRetryCount:          0,
			expectedDuration:            60.0,
		},
		{
			name: "non-terminal to retrying state should emit metrics",
			originalRayJobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusRunning,
				Failed:              pointer.Int32(2),
			},
			rayJobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusRetrying,
				StartTime:           &metav1.Time{Time: mockTime},
			},
			expectMetricsCall:           true,
			expectedJobDeploymentStatus: rayv1.JobDeploymentStatusRetrying,
			expectedRetryCount:          2,
			expectedDuration:            60.0,
		},
		{
			name: "non-terminal to non-terminal state should not emit metrics",
			originalRayJobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusInitializing,
			},
			rayJobStatus: rayv1.RayJobStatus{
				JobDeploymentStatus: rayv1.JobDeploymentStatusRunning,
			},
			expectMetricsCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockObserver := mocks.NewMockRayJobMetricsObserver(ctrl)
			if tt.expectMetricsCall {
				mockObserver.EXPECT().
					ObserveRayJobExecutionDuration(
						rayJobName,
						rayJobNamespace,
						rayJobUID,
						tt.expectedJobDeploymentStatus,
						tt.expectedRetryCount,
						mock.MatchedBy(func(d float64) bool {
							// Allow some wiggle room in timing
							return math.Abs(d-tt.expectedDuration) < 1.0
						}),
					).Times(1)
			}

			emitRayJobExecutionDuration(mockObserver, rayJobName, rayJobNamespace, rayJobUID, tt.originalRayJobStatus, tt.rayJobStatus)
		})
	}
}

func TestConfigureSubmitterContainer(t *testing.T) {
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
								Name:  "ray-head",
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
		Spec: rayv1.RayJobSpec{
			Entrypoint: "python script.py",
			RayClusterSpec: &rayv1.RayClusterSpec{
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: "rayproject/ray",
								},
							},
						},
					},
				},
			},
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://dashboard:8265",
			JobId:        "test-job-id",
		},
	}

	tests := []struct {
		name                  string
		container             corev1.Container
		submissionMode        rayv1.JobSubmissionMode
		expectedContainerName string
	}{
		{
			name: "SidecarMode should always set container name to ray-job-submitter",
			container: corev1.Container{
				Name: "custom-container-name",
			},
			submissionMode:        rayv1.SidecarMode,
			expectedContainerName: utils.SubmitterContainerName,
		},
		{
			name: "SidecarMode with empty container name should set to ray-job-submitter",
			container: corev1.Container{
				Name: "",
			},
			submissionMode:        rayv1.SidecarMode,
			expectedContainerName: utils.SubmitterContainerName,
		},
		{
			name: "K8sJobMode should preserve custom container name",
			container: corev1.Container{
				Name: "my-custom-submitter",
			},
			submissionMode:        rayv1.K8sJobMode,
			expectedContainerName: "my-custom-submitter",
		},
		{
			name: "K8sJobMode with empty container name should remain empty",
			container: corev1.Container{
				Name: "",
			},
			submissionMode:        rayv1.K8sJobMode,
			expectedContainerName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := tt.container.DeepCopy()
			err := configureSubmitterContainer(container, rayJob, rayCluster, tt.submissionMode)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedContainerName, container.Name, "Container name mismatch")
		})
	}
}

func TestConfigureSubmitterContainerImageAndResources(t *testing.T) {
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
								Name:  "ray-head",
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
		Spec: rayv1.RayJobSpec{
			Entrypoint: "python script.py",
			RayClusterSpec: &rayv1.RayClusterSpec{
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: "rayproject/ray",
								},
							},
						},
					},
				},
			},
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "http://dashboard:8265",
			JobId:        "test-job-id",
		},
	}

	t.Run("Empty image should be populated from RayCluster head container", func(t *testing.T) {
		container := &corev1.Container{
			Name: "test-container",
		}
		err := configureSubmitterContainer(container, rayJob, rayCluster, rayv1.K8sJobMode)
		require.NoError(t, err)
		assert.Equal(t, "rayproject/ray", container.Image)
	})

	t.Run("Custom image should be preserved", func(t *testing.T) {
		container := &corev1.Container{
			Name:  "test-container",
			Image: "my-custom-image:latest",
		}
		err := configureSubmitterContainer(container, rayJob, rayCluster, rayv1.K8sJobMode)
		require.NoError(t, err)
		assert.Equal(t, "my-custom-image:latest", container.Image)
	})

	t.Run("Empty resources should get defaults", func(t *testing.T) {
		container := &corev1.Container{
			Name: "test-container",
		}
		err := configureSubmitterContainer(container, rayJob, rayCluster, rayv1.K8sJobMode)
		require.NoError(t, err)
		assert.NotNil(t, container.Resources.Requests)
		assert.NotNil(t, container.Resources.Limits)
	})

	t.Run("Custom resources should be preserved", func(t *testing.T) {
		customResources := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
		container := &corev1.Container{
			Name:      "test-container",
			Resources: customResources,
		}
		err := configureSubmitterContainer(container, rayJob, rayCluster, rayv1.K8sJobMode)
		require.NoError(t, err)
		assert.Equal(t, customResources.Requests[corev1.ResourceCPU], container.Resources.Requests[corev1.ResourceCPU])
		assert.Equal(t, customResources.Requests[corev1.ResourceMemory], container.Resources.Requests[corev1.ResourceMemory])
	})
}
