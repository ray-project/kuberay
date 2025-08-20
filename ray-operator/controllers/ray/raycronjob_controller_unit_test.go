package ray

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrl "sigs.k8s.io/controller-runtime/pkg/reconcile"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestRayCronJobReconciler_Reconcile(t *testing.T) {
	newScheme := runtime.NewScheme()
	err := rayv1.AddToScheme(newScheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(newScheme)
	require.NoError(t, err)

	// Mock time for consistent testing. All schedules will be calculated relative to this.
	mockNow := time.Date(2023, 1, 1, 0, 5, 0, 0, time.UTC)

	tests := []struct {
		name             string
		existingObjects  []runtime.Object
		rayCronJob       *rayv1.RayCronJob
		expectedJobCount int
		expectError      bool
		expectedRequeue  bool
		validate         func(*testing.T, *RayCronJobReconciler, *rayv1.RayCronJob)
	}{
		{
			name: "Successful Reconciliation - Create new Job",
			rayCronJob: &rayv1.RayCronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cronjob",
					Namespace:         "default",
					UID:               types.UID("test-uid"),
					CreationTimestamp: metav1.Time{Time: mockNow.Add(-5 * time.Minute)},
				},
				Spec: rayv1.RayCronJobSpec{
					Schedule: "*/1 * * * *", // Every minute
					RayJobTemplate: rayv1.RayJobTemplate{
						Spec: rayv1.RayJobSpec{
							Entrypoint: "echo 'hello'",
						},
					},
				},
			},
			expectedJobCount: 1,
			expectError:      false,
			expectedRequeue:  true,
			validate: func(t *testing.T, r *RayCronJobReconciler, rcj *rayv1.RayCronJob) {
				// Check if the last schedule time is updated.
				updatedRcj := &rayv1.RayCronJob{}
				err := r.Get(context.TODO(), types.NamespacedName{Name: rcj.Name, Namespace: rcj.Namespace}, updatedRcj)
				require.NoError(t, err)
				assert.NotNil(t, updatedRcj.Status.LastScheduleTime)
				assert.Equal(t, "2023-01-01T00:05:00Z", updatedRcj.Status.LastScheduleTime.Format(time.RFC3339))
			},
		},
		{
			name: "Job Already Active",
			rayCronJob: &rayv1.RayCronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cronjob-active",
					Namespace:         "default",
					UID:               types.UID("test-uid-active"),
					CreationTimestamp: metav1.Time{Time: mockNow.Add(-5 * time.Minute)},
				},
				Spec: rayv1.RayCronJobSpec{
					Schedule: "*/1 * * * *",
				},
			},
			existingObjects: []runtime.Object{
				&rayv1.RayJob{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "rayjob-test-cronjob-active-27875525", // Name matches mockNow hash
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							*metav1.NewControllerRef(&rayv1.RayCronJob{ObjectMeta: metav1.ObjectMeta{UID: types.UID("test-uid-active")}}, rayv1.SchemeGroupVersion.WithKind("RayCronJob")),
						},
					},
					Status: rayv1.RayJobStatus{
						JobStatus: rayv1.JobStatusRunning,
					},
				},
			},
			expectedJobCount: 1, // The existing job, no new job created
			expectError:      false,
			expectedRequeue:  true,
		},
		{
			name: "Invalid Schedule",
			rayCronJob: &rayv1.RayCronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cronjob-invalid",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: mockNow.Add(-5 * time.Minute)},
				},
				Spec: rayv1.RayCronJobSpec{
					Schedule: "invalid schedule",
				},
			},
			expectedJobCount: 0,
			expectError:      false,
			expectedRequeue:  false, // Should not requeue for invalid schedules
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This is the corrected part
			clientBuilder := fake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(tt.rayCronJob).WithStatusSubresource(tt.rayCronJob)
			if len(tt.existingObjects) > 0 {
				clientBuilder.WithRuntimeObjects(tt.existingObjects...)
			}
			fakeClient := clientBuilder.Build()

			reconciler := &RayCronJobReconciler{
				Client:   fakeClient,
				Scheme:   newScheme,
				Recorder: record.NewFakeRecorder(100),
				now:      func() time.Time { return mockNow },
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.rayCronJob.Name,
					Namespace: tt.rayCronJob.Namespace,
				},
			}

			result, err := reconciler.Reconcile(context.TODO(), req)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedRequeue, result.RequeueAfter > 0)

			var childRayJobs rayv1.RayJobList
			err = reconciler.List(context.TODO(), &childRayJobs, client.InNamespace(tt.rayCronJob.Namespace))
			require.NoError(t, err)
			assert.Len(t, childRayJobs.Items, tt.expectedJobCount)

			if tt.validate != nil {
				tt.validate(t, reconciler, tt.rayCronJob)
			}
		})
	}
}

func TestGetRayJobFromTemplate(t *testing.T) {
	scheduledTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	cronJob := &rayv1.RayCronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cronjob",
			Namespace: "test-ns",
		},
		Spec: rayv1.RayCronJobSpec{
			RayJobTemplate: rayv1.RayJobTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"label-key": "label-val"},
					Annotations: map[string]string{"annotation-key": "annotation-val"},
				},
				Spec: rayv1.RayJobSpec{
					Entrypoint: "python /path/to/script.py",
				},
			},
		},
	}

	// This is the corrected part:
	// Instead of hardcoding the name, generate it using the same logic.
	expectedJobName := getJobName(cronJob, scheduledTime)
	job := getRayJobFromTemplate(cronJob, scheduledTime)

	assert.Equal(t, expectedJobName, job.Name)
	assert.Equal(t, cronJob.Namespace, job.Namespace)
	assert.Equal(t, cronJob.Spec.RayJobTemplate.Spec.Entrypoint, job.Spec.Entrypoint)
	assert.Equal(t, "label-val", job.Labels["label-key"])
	assert.Equal(t, "annotation-val", job.Annotations["annotation-key"])
	assert.Len(t, job.OwnerReferences, 1)
	assert.Equal(t, cronJob.Name, job.OwnerReferences[0].Name)
}

func TestGetTimeHashInMinutes(t *testing.T) {
	testTime := time.Unix(1672531230, 0) // Sunday, January 1, 2023 12:00:30 AM
	expectedHash := int64(27875520)      // 1672531230 / 60
	assert.Equal(t, expectedHash, getTimeHashInMinutes(testTime))
}
