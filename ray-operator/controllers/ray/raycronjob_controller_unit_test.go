package ray

import (
	"context"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clocktesting "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func rayCronJobTemplate(name string, namespace string, schedule string) *rayv1.RayCronJob {
	return &rayv1.RayCronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: rayv1.RayCronJobSpec{
			Schedule: schedule,
			JobTemplate: &rayv1.RayJobSpec{
				Entrypoint: "python test.py",
				RayClusterSpec: &rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "ray-head",
										Image: "rayproject/ray:2.9.0",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestRayCronJobReconcile_InvalidSchedule(t *testing.T) {
	ctx := context.Background()

	// Create RayCronJob with invalid cron schedule
	rayCronJob := rayCronJobTemplate("invalid-cronjob", "default", "invalid cron string")

	// Create scheme and add types
	scheme := runtime.NewScheme()
	err := rayv1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create fake client
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(rayCronJob).
		Build()

	// Create fake event recorder with a channel to capture events
	fakeRecorder := record.NewFakeRecorder(10)

	// Create reconciler
	reconciler := &RayCronJobReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: fakeRecorder,
		clock:    clocktesting.NewFakeClock(time.Time{}),
	}

	// Reconcile
	result, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "invalid-cronjob",
			Namespace: "default",
		},
	})

	// Should return no error as validation errors are recorded as events
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that a validation error event was recorded
	select {
	case event := <-fakeRecorder.Events:
		assert.Contains(t, event, "Warning")
		assert.Contains(t, event, "invalid cron schedule")
	default:
		t.Error("Expected a validation error event to be recorded, but none was found")
	}
}

func TestRayCronJobReconcile_FirstSchedule(t *testing.T) {
	ctx := context.Background()

	// Set up test parameters
	cronSchedule := "*/5 * * * *" // Every 5 minutes
	fakeCurrTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create valid RayCronJob
	rayCronJob := rayCronJobTemplate("test-cronjob", "default", cronSchedule)

	// Create scheme and add types
	scheme := runtime.NewScheme()
	err := rayv1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create fake client with status subresource
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(rayCronJob).
		WithStatusSubresource(rayCronJob).
		Build()

	// Create fake clock for deterministic testing
	fakeClock := clocktesting.NewFakeClock(fakeCurrTime)

	// Create reconciler
	reconciler := &RayCronJobReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: &record.FakeRecorder{},
		clock:    fakeClock,
	}

	// Reconcile
	result, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-cronjob",
			Namespace: "default",
		},
	})

	// Should succeed with a requeue time
	require.NoError(t, err)
	assert.Positive(t, result.RequeueAfter, "Should requeue for next schedule")

	// Parse the cron schedule to get the expected next schedule time
	schedule, err := cron.ParseStandard(cronSchedule)
	require.NoError(t, err, "Test cron schedule should be valid")
	expectedNextSchedule := schedule.Next(fakeCurrTime)
	expectedRequeueAfter := expectedNextSchedule.Sub(fakeCurrTime)

	// Verify the requeue time matches the cron schedule
	assert.Equal(t, expectedRequeueAfter, result.RequeueAfter,
		"RequeueAfter should match the time until next schedule based on cron expression")

	// Check that LastScheduleTime was set to current time
	updatedCronJob := &rayv1.RayCronJob{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      "test-cronjob",
		Namespace: "default",
	}, updatedCronJob)
	require.NoError(t, err)
	assert.NotNil(t, updatedCronJob.Status.LastScheduleTime)
	assert.True(t, fakeCurrTime.Equal(updatedCronJob.Status.LastScheduleTime.Time),
		"LastScheduleTime should be set to current time. Expected: %v, Got: %v",
		fakeCurrTime, updatedCronJob.Status.LastScheduleTime.Time)
}

func TestRayCronJobReconcile_CreateRayJob(t *testing.T) {
	ctx := context.Background()

	// Set up test parameters
	cronSchedule := "*/5 * * * *" // Every 5 minutes
	fakeCurrTime := time.Date(2024, 1, 1, 0, 10, 0, 0, time.UTC)
	lastScheduleTime := metav1.NewTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	rayCronJob := rayCronJobTemplate("test-cronjob", "default", cronSchedule)
	rayCronJob.Status = rayv1.RayCronJobStatus{
		LastScheduleTime: &lastScheduleTime,
	}

	// Create scheme and add types
	scheme := runtime.NewScheme()
	err := rayv1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create fake client
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(rayCronJob).
		WithStatusSubresource(rayCronJob).
		Build()

	// Create fake clock set to a time that should trigger job creation
	fakeClock := clocktesting.NewFakeClock(fakeCurrTime)

	// Create reconciler
	reconciler := &RayCronJobReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: &record.FakeRecorder{},
		clock:    fakeClock,
	}

	// Reconcile
	result, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-cronjob",
			Namespace: "default",
		},
	})

	// Should succeed with a requeue time
	require.NoError(t, err)
	assert.Positive(t, result.RequeueAfter, "Should requeue for next schedule")

	// Parse the cron schedule to get the expected next schedule time
	schedule, err := cron.ParseStandard(cronSchedule)
	require.NoError(t, err, "Test cron schedule should be valid")
	expectedNextSchedule := schedule.Next(fakeCurrTime)
	expectedRequeueAfter := expectedNextSchedule.Sub(fakeCurrTime)

	// Verify the requeue time matches the cron schedule
	assert.Equal(t, expectedRequeueAfter, result.RequeueAfter,
		"RequeueAfter should match the time until next schedule based on cron expression")

	// Verify a RayJob was created
	rayJobList := &rayv1.RayJobList{}
	err = fakeClient.List(ctx, rayJobList)
	require.NoError(t, err)
	assert.Len(t, rayJobList.Items, 1, "Should have created one RayJob")

	// Verify RayJob has correct label
	rayJob := rayJobList.Items[0]
	assert.Equal(t, "test-cronjob", rayJob.Labels["ray.io/cronjob-name"])
	assert.Equal(t, "python test.py", rayJob.Spec.Entrypoint)

	// Verify LastScheduleTime was updated to current time
	updatedCronJob := &rayv1.RayCronJob{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      "test-cronjob",
		Namespace: "default",
	}, updatedCronJob)
	require.NoError(t, err)
	assert.NotNil(t, updatedCronJob.Status.LastScheduleTime)
	assert.True(t, fakeCurrTime.Equal(updatedCronJob.Status.LastScheduleTime.Time),
		"LastScheduleTime should be updated to current time after creating job. Expected: %v, Got: %v",
		fakeCurrTime, updatedCronJob.Status.LastScheduleTime.Time)
}

func TestUpdateRayCronJobStatus(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		oldCronJob   *rayv1.RayCronJob
		newCronJob   *rayv1.RayCronJob
		name         string
		shouldUpdate bool
	}{
		{
			name: "LastScheduleTime changed",
			oldCronJob: &rayv1.RayCronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cronjob",
					Namespace: "default",
				},
				Status: rayv1.RayCronJobStatus{
					LastScheduleTime: &metav1.Time{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
				},
			},
			newCronJob: &rayv1.RayCronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cronjob",
					Namespace: "default",
				},
				Status: rayv1.RayCronJobStatus{
					LastScheduleTime: &metav1.Time{Time: time.Date(2024, 1, 1, 0, 5, 0, 0, time.UTC)},
				},
			},
			shouldUpdate: true,
		},
		{
			name: "LastScheduleTime unchanged",
			oldCronJob: &rayv1.RayCronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cronjob",
					Namespace: "default",
				},
				Status: rayv1.RayCronJobStatus{
					LastScheduleTime: &metav1.Time{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
				},
			},
			newCronJob: &rayv1.RayCronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cronjob",
					Namespace: "default",
				},
				Status: rayv1.RayCronJobStatus{
					LastScheduleTime: &metav1.Time{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
				},
			},
			shouldUpdate: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create scheme
			scheme := runtime.NewScheme()
			err := rayv1.AddToScheme(scheme)
			require.NoError(t, err)

			// Create fake client - start with old status
			initialCronJob := tc.oldCronJob.DeepCopy()
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(initialCronJob).
				WithStatusSubresource(initialCronJob).
				Build()

			// Create reconciler
			reconciler := &RayCronJobReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: &record.FakeRecorder{},
			}

			// Fetch the current object from the client to get valid resourceVersion
			currentCronJob := &rayv1.RayCronJob{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-cronjob",
				Namespace: "default",
			}, currentCronJob)
			require.NoError(t, err)

			// Update the status of the fetched object to match the new status
			currentCronJob.Status = tc.newCronJob.Status

			// Call updateRayCronJobStatus with the fetched object that has valid resourceVersion
			err = reconciler.updateRayCronJobStatus(ctx, tc.oldCronJob, currentCronJob)
			require.NoError(t, err)

			updatedCronJob := &rayv1.RayCronJob{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-cronjob",
				Namespace: "default",
			}, updatedCronJob)
			require.NoError(t, err)
			assert.NotNil(t, updatedCronJob.Status.LastScheduleTime)

			if tc.shouldUpdate {
				// Verify the status was updated to the new value
				assert.True(t, tc.newCronJob.Status.LastScheduleTime.Time.Equal(updatedCronJob.Status.LastScheduleTime.Time),
					"Status should be updated to new LastScheduleTime. Expected: %v, Got: %v",
					tc.newCronJob.Status.LastScheduleTime.Time, updatedCronJob.Status.LastScheduleTime.Time)
			} else {
				// Verify the status remained unchanged
				assert.True(t, tc.oldCronJob.Status.LastScheduleTime.Time.Equal(updatedCronJob.Status.LastScheduleTime.Time),
					"Status should remain unchanged. Expected: %v, Got: %v",
					tc.oldCronJob.Status.LastScheduleTime.Time, updatedCronJob.Status.LastScheduleTime.Time)
			}
		})
	}
}
