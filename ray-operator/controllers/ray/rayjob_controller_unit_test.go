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
	"k8s.io/utils/ptr"
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
				Failed:              ptr.To(int32(2)),
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

func TestGetSubmitterTemplate_WithEnableK8sTokenAuth(t *testing.T) {
	rayJob := &rayv1.RayJob{
		Spec: rayv1.RayJobSpec{
			SubmissionMode: rayv1.K8sJobMode,
		},
	}
	rayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-raycluster",
		},
		Spec: rayv1.RayClusterSpec{
			AuthOptions: &rayv1.AuthOptions{
				Mode:               rayv1.AuthModeToken,
				EnableK8sTokenAuth: ptr.To(true),
			},
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

	template, err := getSubmitterTemplate(rayJob, rayCluster)
	require.NoError(t, err)

	// Check volume
	foundVolume := false
	for _, v := range template.Spec.Volumes {
		if v.Name == utils.RayTokenVolumeName && v.Projected != nil {
			foundVolume = true
			break
		}
	}
	assert.True(t, foundVolume, "Submitter Pod should have the ray-token volume")

	// Check volume mount
	foundVolumeMount := false
	for _, vm := range template.Spec.Containers[utils.RayContainerIndex].VolumeMounts {
		if vm.Name == utils.RayTokenVolumeName && vm.MountPath == utils.RayTokenMountPath {
			foundVolumeMount = true
			break
		}
	}
	assert.True(t, foundVolumeMount, "Submitter container should have the ray-token volume mount")
}

// newSidecarModeRayJob creates a base RayJob configured for SidecarMode testing.
// If submitter is non-nil, it's added as a user-provided submitter container in the head pod.
func newSidecarModeRayJob(submitter *corev1.Container) *rayv1.RayJob {
	containers := []corev1.Container{
		{Name: "ray-head", Image: "rayproject/ray:2.9.0"},
	}
	if submitter != nil {
		containers = append(containers, *submitter)
	}
	return &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{Name: "test-rayjob", Namespace: "default"},
		Spec: rayv1.RayJobSpec{
			SubmissionMode: rayv1.SidecarMode,
			Entrypoint:     "python test.py",
			RayClusterSpec: &rayv1.RayClusterSpec{
				HeadGroupSpec: rayv1.HeadGroupSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: containers},
					},
				},
			},
		},
		Status: rayv1.RayJobStatus{
			DashboardURL: "test-url",
			JobId:        "test-job-id",
		},
	}
}

// constructSidecarModeCluster runs the reconciler on a SidecarMode RayJob and returns the resulting RayCluster.
func constructSidecarModeCluster(t *testing.T, rayJob *rayv1.RayJob) *rayv1.RayCluster {
	t.Helper()
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	reconciler := &RayJobReconciler{Scheme: newScheme}
	rayCluster, err := reconciler.constructRayClusterForRayJob(rayJob, "test-raycluster")
	require.NoError(t, err)
	return rayCluster
}

func TestConstructRayClusterForRayJob_SidecarMode_DefaultContainer(t *testing.T) {
	// When no user-provided ray-job-submitter container exists, the controller
	// should create a default sidecar and append it to the head pod containers.
	rayJob := newSidecarModeRayJob(nil)
	rayCluster := constructSidecarModeCluster(t, rayJob)

	// Should have 2 containers: ray-head + ray-job-submitter
	require.Len(t, rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers, 2)
	assert.Equal(t, "ray-head", rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Name)
	assert.Equal(t, utils.SubmitterContainerName, rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1].Name)

	// Default sidecar should use the ray head image
	assert.Equal(t, "rayproject/ray:2.9.0", rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1].Image)

	// RestartPolicy should be set to Never
	assert.Equal(t, corev1.RestartPolicyNever, rayCluster.Spec.HeadGroupSpec.Template.Spec.RestartPolicy)

	// Annotation should be set
	assert.Equal(t, "true", rayCluster.Annotations[utils.DisableProvisionedHeadRestartAnnotationKey])

	// Env vars should be injected
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]
	envVar, found := utils.EnvVarByName(PythonUnbufferedEnvVarName, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "1", envVar.Value)

	envVar, found = utils.EnvVarByName(utils.RAY_DASHBOARD_ADDRESS, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "test-url", envVar.Value)

	envVar, found = utils.EnvVarByName(utils.RAY_JOB_SUBMISSION_ID, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "test-job-id", envVar.Value)
}

func TestConstructRayClusterForRayJob_SidecarMode_UserProvidedContainer(t *testing.T) {
	// When the user provides a container named "ray-job-submitter" in the head pod spec,
	// the controller should configure it in place rather than appending a new one.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		VolumeMounts: []corev1.VolumeMount{
			{Name: "job-scripts", MountPath: "/scripts"},
		},
		Env: []corev1.EnvVar{
			{Name: "MY_CUSTOM_VAR", Value: "hello"},
		},
	})
	rayJob.Spec.Entrypoint = "python /scripts/my_job.py"
	rayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "job-scripts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-job-scripts"},
				},
			},
		},
	}

	rayCluster := constructSidecarModeCluster(t, rayJob)

	// Should still have exactly 2 containers — no duplicate appended
	require.Len(t, rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers, 2)
	assert.Equal(t, "ray-head", rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Name)
	assert.Equal(t, utils.SubmitterContainerName, rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1].Name)

	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// User's custom image should be preserved
	assert.Equal(t, "my-custom-image:latest", submitter.Image)

	// User's volume mounts should be preserved
	require.Len(t, submitter.VolumeMounts, 1)
	assert.Equal(t, "job-scripts", submitter.VolumeMounts[0].Name)
	assert.Equal(t, "/scripts", submitter.VolumeMounts[0].MountPath)

	// User's env vars should be preserved, with controller env vars appended
	envVar, found := utils.EnvVarByName("MY_CUSTOM_VAR", submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "hello", envVar.Value)

	envVar, found = utils.EnvVarByName(PythonUnbufferedEnvVarName, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "1", envVar.Value)

	envVar, found = utils.EnvVarByName(utils.RAY_DASHBOARD_ADDRESS, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "test-url", envVar.Value)

	envVar, found = utils.EnvVarByName(utils.RAY_JOB_SUBMISSION_ID, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "test-job-id", envVar.Value)

	// Command should be overwritten by the controller
	assert.Equal(t, []string{"/bin/bash", "-ce", "--"}, submitter.Command)
	assert.Len(t, submitter.Args, 1)

	// RestartPolicy should be set to Never
	assert.Equal(t, corev1.RestartPolicyNever, rayCluster.Spec.HeadGroupSpec.Template.Spec.RestartPolicy)
}

func TestConstructRayClusterForRayJob_SidecarMode_UserProvidedContainer_ImageDefaulting(t *testing.T) {
	// When the user provides a ray-job-submitter container without an image,
	// the controller should default to the Ray head image.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name: utils.SubmitterContainerName,
		// No image specified — should default to ray head image
		VolumeMounts: []corev1.VolumeMount{
			{Name: "job-scripts", MountPath: "/scripts"},
		},
	})

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// Image should default to the ray head image
	assert.Equal(t, "rayproject/ray:2.9.0", submitter.Image)

	// Volume mounts should be preserved
	require.Len(t, submitter.VolumeMounts, 1)
	assert.Equal(t, "job-scripts", submitter.VolumeMounts[0].Name)
}

func TestConstructRayClusterForRayJob_SidecarMode_UserProvidedContainer_ResourceDefaulting(t *testing.T) {
	// When the user provides a ray-job-submitter container without resources,
	// the controller should apply default resource limits.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		// No resources specified — should get defaults
	})

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// Default resources should be applied
	assert.NotNil(t, submitter.Resources.Limits)
	assert.NotNil(t, submitter.Resources.Requests)
}

func TestConstructRayClusterForRayJob_SidecarMode_UserProvidedContainer_CustomResources(t *testing.T) {
	// When the user specifies custom resources, they should be preserved.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	})

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// User-specified resources should be preserved
	assert.True(t, submitter.Resources.Requests.Cpu().Equal(resource.MustParse("200m")))
	assert.True(t, submitter.Resources.Requests.Memory().Equal(resource.MustParse("256Mi")))
	assert.True(t, submitter.Resources.Limits.Cpu().Equal(resource.MustParse("500m")))
	assert.True(t, submitter.Resources.Limits.Memory().Equal(resource.MustParse("512Mi")))
}

func TestConstructRayClusterForRayJob_SidecarMode_UserProvidedContainer_PartialResources_OnlyRequests(t *testing.T) {
	// When the user specifies only Requests (no Limits), defaults are NOT applied.
	// The user's partial resource spec is preserved as-is.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
	})

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// User's Requests should be preserved
	assert.True(t, submitter.Resources.Requests.Cpu().Equal(resource.MustParse("100m")))
	assert.True(t, submitter.Resources.Requests.Memory().Equal(resource.MustParse("128Mi")))

	// Limits should remain nil (no defaults applied for partial spec)
	assert.Nil(t, submitter.Resources.Limits)
}

func TestConstructRayClusterForRayJob_SidecarMode_UserProvidedContainer_PartialResources_OnlyLimits(t *testing.T) {
	// When the user specifies only Limits (no Requests), defaults are NOT applied.
	// The user's partial resource spec is preserved as-is.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	})

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// Requests should remain nil (no defaults applied for partial spec)
	assert.Nil(t, submitter.Resources.Requests)

	// User's Limits should be preserved
	assert.True(t, submitter.Resources.Limits.Cpu().Equal(resource.MustParse("500m")))
	assert.True(t, submitter.Resources.Limits.Memory().Equal(resource.MustParse("512Mi")))
}

func TestConstructRayClusterForRayJob_SidecarMode_UserEnvVarsPreserved(t *testing.T) {
	// Verify that user-defined env vars on the user-provided submitter container are preserved
	// and controller-injected env vars are appended (not replacing user vars).
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		Env: []corev1.EnvVar{
			{Name: "USER_VAR_1", Value: "value1"},
			{Name: "USER_VAR_2", Value: "value2"},
			{Name: "API_KEY", Value: "secret-key"},
		},
	})

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// All user-defined env vars should be preserved
	envVar, found := utils.EnvVarByName("USER_VAR_1", submitter.Env)
	assert.True(t, found, "USER_VAR_1 should be preserved")
	assert.Equal(t, "value1", envVar.Value)

	envVar, found = utils.EnvVarByName("USER_VAR_2", submitter.Env)
	assert.True(t, found, "USER_VAR_2 should be preserved")
	assert.Equal(t, "value2", envVar.Value)

	envVar, found = utils.EnvVarByName("API_KEY", submitter.Env)
	assert.True(t, found, "API_KEY should be preserved")
	assert.Equal(t, "secret-key", envVar.Value)

	// Controller-injected env vars should also be present
	_, found = utils.EnvVarByName(utils.RAY_DASHBOARD_ADDRESS, submitter.Env)
	assert.True(t, found, "RAY_DASHBOARD_ADDRESS should be injected")
	_, found = utils.EnvVarByName(utils.RAY_JOB_SUBMISSION_ID, submitter.Env)
	assert.True(t, found, "RAY_JOB_SUBMISSION_ID should be injected")
	_, found = utils.EnvVarByName(PythonUnbufferedEnvVarName, submitter.Env)
	assert.True(t, found, "PYTHONUNBUFFERED should be injected")

	// Total env count: 3 user + 3 controller-injected
	assert.Len(t, submitter.Env, 6)
}

func TestConstructRayClusterForRayJob_SidecarMode_ControllerEnvVarsOverrideUserValues(t *testing.T) {
	// Verify that when a user pre-defines controller-managed env vars (RAY_DASHBOARD_ADDRESS,
	// RAY_JOB_SUBMISSION_ID, PYTHONUNBUFFERED) on the user-provided submitter container,
	// the controller replaces them with the correct runtime values instead of creating duplicates.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		Env: []corev1.EnvVar{
			{Name: "USER_VAR", Value: "keep-me"},
			{Name: utils.RAY_DASHBOARD_ADDRESS, Value: "user-should-be-overridden"},
			{Name: utils.RAY_JOB_SUBMISSION_ID, Value: "user-should-be-overridden"},
			{Name: PythonUnbufferedEnvVarName, Value: "0"},
		},
	})
	rayJob.Status.DashboardURL = "correct-dashboard-url"
	rayJob.Status.JobId = "correct-job-id"

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// Controller-managed env vars should have the controller's values, not the user's
	envVar, found := utils.EnvVarByName(utils.RAY_DASHBOARD_ADDRESS, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "correct-dashboard-url", envVar.Value, "RAY_DASHBOARD_ADDRESS should be overridden with controller value")

	envVar, found = utils.EnvVarByName(utils.RAY_JOB_SUBMISSION_ID, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "correct-job-id", envVar.Value, "RAY_JOB_SUBMISSION_ID should be overridden with controller value")

	envVar, found = utils.EnvVarByName(PythonUnbufferedEnvVarName, submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "1", envVar.Value, "PYTHONUNBUFFERED should be overridden with controller value")

	// User-defined env var should still be preserved
	envVar, found = utils.EnvVarByName("USER_VAR", submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "keep-me", envVar.Value)

	// No duplicates — should be exactly 4 env vars (1 user + 3 controller-managed, replaced in place)
	assert.Len(t, submitter.Env, 4)
}

func TestConstructRayClusterForRayJob_SidecarMode_CommandAlwaysOverwritten(t *testing.T) {
	// Verify that even if the user sets Command and Args on the user-provided submitter container,
	// the controller overwrites them in SidecarMode.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:    utils.SubmitterContainerName,
		Image:   "my-custom-image:latest",
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{"echo user-command"},
	})

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// Command should be overwritten to the controller's default, not the user's
	assert.Equal(t, []string{"/bin/bash", "-ce", "--"}, submitter.Command)
	assert.NotContains(t, submitter.Args[0], "echo user-command", "user args should be overwritten")
	assert.Contains(t, submitter.Args[0], "ray job submit", "controller should inject ray job submit command")
}

func TestConstructRayClusterForRayJob_SidecarMode_UserVolumeMountsPreserved(t *testing.T) {
	// Verify that user-defined volume mounts on the user-provided submitter container are preserved
	// since configureSubmitterContainer never touches VolumeMounts.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		VolumeMounts: []corev1.VolumeMount{
			{Name: "scripts", MountPath: "/opt/scripts", ReadOnly: true},
			{Name: "creds", MountPath: "/etc/creds", ReadOnly: true},
		},
	})
	rayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "scripts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-scripts"},
				},
			},
		},
		{
			Name: "creds",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "my-creds"},
			},
		},
	}

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// Both user-defined volume mounts should be preserved
	require.Len(t, submitter.VolumeMounts, 2)

	assert.Equal(t, "scripts", submitter.VolumeMounts[0].Name)
	assert.Equal(t, "/opt/scripts", submitter.VolumeMounts[0].MountPath)
	assert.True(t, submitter.VolumeMounts[0].ReadOnly)

	assert.Equal(t, "creds", submitter.VolumeMounts[1].Name)
	assert.Equal(t, "/etc/creds", submitter.VolumeMounts[1].MountPath)
	assert.True(t, submitter.VolumeMounts[1].ReadOnly)
}

func TestConstructRayClusterForRayJob_SidecarMode_UserProvidedContainer_WithAuthToken(t *testing.T) {
	// Verify that when auth is enabled, the controller injects auth env vars into
	// a user-provided submitter container without conflicting with user-defined
	// env vars or volume mounts.
	rayJob := newSidecarModeRayJob(&corev1.Container{
		Name:  utils.SubmitterContainerName,
		Image: "my-custom-image:latest",
		Env: []corev1.EnvVar{
			{Name: "USER_VAR", Value: "keep-me"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "user-mount", MountPath: "/data"},
		},
	})
	rayJob.Spec.RayClusterSpec.AuthOptions = &rayv1.AuthOptions{
		Mode: rayv1.AuthModeToken,
	}

	rayCluster := constructSidecarModeCluster(t, rayJob)
	submitter := rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[1]

	// User env var preserved
	envVar, found := utils.EnvVarByName("USER_VAR", submitter.Env)
	assert.True(t, found)
	assert.Equal(t, "keep-me", envVar.Value)

	// Controller-injected env vars present
	_, found = utils.EnvVarByName(utils.RAY_DASHBOARD_ADDRESS, submitter.Env)
	assert.True(t, found, "RAY_DASHBOARD_ADDRESS should be injected")
	_, found = utils.EnvVarByName(utils.RAY_JOB_SUBMISSION_ID, submitter.Env)
	assert.True(t, found, "RAY_JOB_SUBMISSION_ID should be injected")
	_, found = utils.EnvVarByName(PythonUnbufferedEnvVarName, submitter.Env)
	assert.True(t, found, "PYTHONUNBUFFERED should be injected")

	// Auth env vars should be injected
	envVar, found = utils.EnvVarByName(utils.RAY_AUTH_MODE_ENV_VAR, submitter.Env)
	assert.True(t, found, "RAY_AUTH_MODE should be injected")
	assert.Equal(t, string(rayv1.AuthModeToken), envVar.Value)

	envVar, found = utils.EnvVarByName(utils.RAY_AUTH_TOKEN_ENV_VAR, submitter.Env)
	assert.True(t, found, "RAY_AUTH_TOKEN should be injected")
	assert.NotNil(t, envVar.ValueFrom, "RAY_AUTH_TOKEN should reference a secret")
	assert.NotNil(t, envVar.ValueFrom.SecretKeyRef)
	assert.Equal(t, utils.RAY_AUTH_TOKEN_SECRET_KEY, envVar.ValueFrom.SecretKeyRef.Key)

	// User volume mount preserved
	assert.Equal(t, "user-mount", submitter.VolumeMounts[0].Name)
	assert.Equal(t, "/data", submitter.VolumeMounts[0].MountPath)
}
