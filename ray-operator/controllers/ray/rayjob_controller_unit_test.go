package ray

import (
	"context"
	"errors"
	"fmt"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics/mocks"
	utils "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
)

// fakeBatchScheduler implements schedulerinterface.BatchScheduler for testing.
type fakeBatchScheduler struct {
	schedulerinterface.DefaultBatchScheduler
	cleanupCalled    bool
	cleanupObject    metav1.Object
	cleanupDidUpdate bool
	cleanupErr       error
}

func (f *fakeBatchScheduler) CleanupOnCompletion(_ context.Context, object metav1.Object) (bool, error) {
	f.cleanupCalled = true
	f.cleanupObject = object
	return f.cleanupDidUpdate, f.cleanupErr
}

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
	expectedK8sJobModeArgs := []string{
		"until " + fmt.Sprintf(utils.BasePythonHealthCommand, "http://test-url/"+utils.RayDashboardGCSHealthPath, utils.DefaultReadinessProbeFailureThreshold) +
			" >/dev/null 2>&1 ; do echo \"Waiting for Ray Dashboard GCS to become healthy at http://test-url ...\" ; sleep 2 ; done ; " +
			"if ! ray job status --address http://test-url test-job-id >/dev/null 2>&1 ; then ray job submit --address http://test-url --no-wait --submission-id test-job-id -- echo no quote 'single quote' \"double quote\" ; fi ; ray job logs --address http://test-url --follow test-job-id",
	}
	assert.Equal(t, expectedK8sJobModeArgs, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Args)

	// Test 3: User did not provide template, should use the image of the Ray Head
	submitterTemplate, err = getSubmitterTemplate(rayJobInstanceWithoutTemplate, rayClusterInstance)
	require.NoError(t, err)
	assert.Equal(t, []string{"/bin/bash", "-ce", "--"}, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Command)
	assert.Equal(t, expectedK8sJobModeArgs, submitterTemplate.Spec.Containers[utils.RayContainerIndex].Args)
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
				Failed:              new(int32(2)),
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
				EnableK8sTokenAuth: new(true),
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

func TestBatchSchedulerOnCompletionCalledWhenRayJobComplete(t *testing.T) {
	tests := []struct {
		name                string
		jobDeploymentStatus rayv1.JobDeploymentStatus
		cleanupDidUpdate    bool
		cleanupErr          error
		expectCleanupCalled bool
		expectCleanupEvent  string
	}{
		{
			name:                "Complete status - cleanup performed successfully",
			jobDeploymentStatus: rayv1.JobDeploymentStatusComplete,
			cleanupDidUpdate:    true,
			cleanupErr:          nil,
			expectCleanupCalled: true,
			expectCleanupEvent:  string(utils.BatchSchedulerCleanedUp),
		},
		{
			name:                "Complete status - cleanup no-op (no resources to clean)",
			jobDeploymentStatus: rayv1.JobDeploymentStatusComplete,
			cleanupDidUpdate:    false,
			cleanupErr:          nil,
			expectCleanupCalled: true,
			expectCleanupEvent:  "",
		},
		{
			name:                "Complete status - cleanup returns error",
			jobDeploymentStatus: rayv1.JobDeploymentStatusComplete,
			cleanupDidUpdate:    false,
			cleanupErr:          errors.New("cleanup failed"),
			expectCleanupCalled: true,
			expectCleanupEvent:  string(utils.FailedToCleanupBatchScheduler),
		},
		{
			name:                "Failed status - cleanup performed successfully",
			jobDeploymentStatus: rayv1.JobDeploymentStatusFailed,
			cleanupDidUpdate:    true,
			cleanupErr:          nil,
			expectCleanupCalled: true,
			expectCleanupEvent:  string(utils.BatchSchedulerCleanedUp),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newScheme := runtime.NewScheme()
			_ = rayv1.AddToScheme(newScheme)

			rayJob := &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rayjob",
					Namespace: "default",
				},
				Spec: rayv1.RayJobSpec{
					Entrypoint: "echo hello",
					RayClusterSpec: &rayv1.RayClusterSpec{
						HeadGroupSpec: rayv1.HeadGroupSpec{
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  "ray-head",
											Image: "rayproject/ray:latest",
										},
									},
								},
							},
						},
					},
				},
				Status: rayv1.RayJobStatus{
					JobDeploymentStatus: tc.jobDeploymentStatus,
					JobStatus:           rayv1.JobStatusSucceeded,
					RayClusterName:      "test-raycluster",
				},
			}

			fakeClient := clientFake.NewClientBuilder().
				WithScheme(newScheme).
				WithRuntimeObjects(rayJob).
				WithStatusSubresource(rayJob).
				Build()

			fakeScheduler := &fakeBatchScheduler{
				cleanupDidUpdate: tc.cleanupDidUpdate,
				cleanupErr:       tc.cleanupErr,
			}
			schedulerManager := batchscheduler.NewSchedulerManagerForTest(fakeScheduler)

			recorder := record.NewFakeRecorder(100)

			reconciler := &RayJobReconciler{
				Client:   fakeClient,
				Recorder: recorder,
				Scheme:   newScheme,
				options: RayJobReconcilerOptions{
					BatchSchedulerManager: schedulerManager,
				},
			}

			ctx := context.Background()
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      rayJob.Name,
					Namespace: rayJob.Namespace,
				},
			})
			// The Reconcile should not return an error for terminal states
			require.NoError(t, err)

			// Verify CleanupOnCompletion was called
			assert.True(t, fakeScheduler.cleanupCalled, "CleanupOnCompletion should have been called when RayJob is in %s status", tc.jobDeploymentStatus)
			assert.Equal(t, rayJob.Name, fakeScheduler.cleanupObject.GetName(), "CleanupOnCompletion should receive the correct RayJob object")
			assert.Equal(t, rayJob.Namespace, fakeScheduler.cleanupObject.GetNamespace(), "CleanupOnCompletion should receive the correct RayJob namespace")

			// Verify the expected event was emitted
			if tc.expectCleanupEvent != "" {
				var foundEvent bool
				for len(recorder.Events) > 0 {
					event := <-recorder.Events
					if strings.Contains(event, tc.expectCleanupEvent) {
						foundEvent = true
						break
					}
				}
				assert.True(t, foundEvent, "Expected event %q to be emitted", tc.expectCleanupEvent)
			}
		})
	}
}
