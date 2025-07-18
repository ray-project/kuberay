package utils

import (
	"reflect"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	cron "github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMostRecentScheduleTime(t *testing.T) {
	metav1TopOfTheHour := metav1.NewTime(*topOfTheHour())
	metav1HalfPastTheHour := metav1.NewTime(*deltaTimeAfterTopOfTheHour(30 * time.Minute))

	tests := []struct {
		name                  string
		cj                    *rayv1.RayJob
		includeSDS            bool
		now                   time.Time
		expectedEarliestTime  time.Time
		expectedRecentTime    *time.Time
		expectedTooManyMissed missedSchedulesType
		wantErr               bool
	}{
		{
			name: "now before next schedule",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 * * * *",
				},
			},
			now:                  topOfTheHour().Add(30 * time.Second),
			expectedRecentTime:   nil,
			expectedEarliestTime: *topOfTheHour(),
		},
		{
			name: "now just after next schedule",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 * * * *",
				},
			},
			now:                  topOfTheHour().Add(61 * time.Minute),
			expectedRecentTime:   deltaTimeAfterTopOfTheHour(60 * time.Minute),
			expectedEarliestTime: *topOfTheHour(),
		},
		{
			name: "missed 5 schedules",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(*deltaTimeAfterTopOfTheHour(10 * time.Second)),
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 * * * *",
				},
			},
			now:                   *deltaTimeAfterTopOfTheHour(301 * time.Minute),
			expectedRecentTime:    deltaTimeAfterTopOfTheHour(300 * time.Minute),
			expectedEarliestTime:  *deltaTimeAfterTopOfTheHour(10 * time.Second),
			expectedTooManyMissed: fewMissed,
		},
		{
			name: "complex schedule",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "30 6-16/4 * * 1-5",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1HalfPastTheHour,
				},
			},
			now:                   *deltaTimeAfterTopOfTheHour(24*time.Hour + 31*time.Minute),
			expectedRecentTime:    deltaTimeAfterTopOfTheHour(24*time.Hour + 30*time.Minute),
			expectedEarliestTime:  *deltaTimeAfterTopOfTheHour(30 * time.Minute),
			expectedTooManyMissed: fewMissed,
		},
		{
			name: "another complex schedule",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "30 10,11,12 * * 1-5",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1HalfPastTheHour,
				},
			},
			now:                   *deltaTimeAfterTopOfTheHour(30*time.Hour + 30*time.Minute),
			expectedRecentTime:    nil,
			expectedEarliestTime:  *deltaTimeAfterTopOfTheHour(30 * time.Minute),
			expectedTooManyMissed: fewMissed,
		},
		{
			name: "complex schedule with longer diff between executions",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "30 6-16/4 * * 1-5",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1HalfPastTheHour,
				},
			},
			now:                   *deltaTimeAfterTopOfTheHour(96*time.Hour + 31*time.Minute),
			expectedRecentTime:    deltaTimeAfterTopOfTheHour(96*time.Hour + 30*time.Minute),
			expectedEarliestTime:  *deltaTimeAfterTopOfTheHour(30 * time.Minute),
			expectedTooManyMissed: fewMissed,
		},
		{
			name: "complex schedule with shorter diff between executions",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "30 6-16/4 * * 1-5",
				},
			},
			now:                   *deltaTimeAfterTopOfTheHour(24*time.Hour + 31*time.Minute),
			expectedRecentTime:    deltaTimeAfterTopOfTheHour(24*time.Hour + 30*time.Minute),
			expectedEarliestTime:  *topOfTheHour(),
			expectedTooManyMissed: fewMissed,
		},
		{
			name: "rogue cronjob",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(*deltaTimeAfterTopOfTheHour(10 * time.Second)),
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "59 23 31 2 *",
				},
			},
			now:                *deltaTimeAfterTopOfTheHour(1 * time.Hour),
			expectedRecentTime: nil,
			wantErr:            true,
		},
		{
			name: "earliestTime being CreationTimestamp and LastScheduleTime",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 * * * *",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1TopOfTheHour,
				},
			},
			now:                  *deltaTimeAfterTopOfTheHour(30 * time.Second),
			expectedEarliestTime: *topOfTheHour(),
			expectedRecentTime:   nil,
		},
		{
			name: "earliestTime being LastScheduleTime",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "*/5 * * * *",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1HalfPastTheHour,
				},
			},
			now:                  *deltaTimeAfterTopOfTheHour(31 * time.Minute),
			expectedEarliestTime: *deltaTimeAfterTopOfTheHour(30 * time.Minute),
			expectedRecentTime:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := cron.ParseStandard(tt.cj.Spec.Schedule)
			if err != nil {
				t.Errorf("error setting up the test, %s", err)
			}
			gotEarliestTime, gotRecentTime, gotTooManyMissed, err := MostRecentScheduleTime(tt.cj, tt.now, sched)
			if tt.wantErr {
				if err == nil {
					t.Error("mostRecentScheduleTime() got no error when expected one")
				}
				return
			}
			if !tt.wantErr && err != nil {
				t.Error("mostRecentScheduleTime() got error when none expected")
			}
			if gotEarliestTime.IsZero() {
				t.Errorf("earliestTime should never be 0, want %v", tt.expectedEarliestTime)
			}
			if !gotEarliestTime.Equal(tt.expectedEarliestTime) {
				t.Errorf("expectedEarliestTime - got %v, want %v", gotEarliestTime, tt.expectedEarliestTime)
			}
			if !reflect.DeepEqual(gotRecentTime, tt.expectedRecentTime) {
				t.Errorf("expectedRecentTime - got %v, want %v", gotRecentTime, tt.expectedRecentTime)
			}
			if gotTooManyMissed != tt.expectedTooManyMissed {
				t.Errorf("expectedNumberOfMisses - got %v, want %v", gotTooManyMissed, tt.expectedTooManyMissed)
			}
		})
	}
}

func TestNextScheduleTimeDuration(t *testing.T) {
	logger := testr.New(t)
	metav1TopOfTheHour := metav1.NewTime(*topOfTheHour())
	metav1HalfPastTheHour := metav1.NewTime(*deltaTimeAfterTopOfTheHour(30 * time.Minute))
	metav1TwoHoursLater := metav1.NewTime(*deltaTimeAfterTopOfTheHour(2 * time.Hour))

	tests := []struct {
		name             string
		cj               *rayv1.RayJob
		now              time.Time
		expectedDuration time.Duration
	}{
		{
			name: "complex schedule skipping weekend",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "30 6-16/4 * * 1-5",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1HalfPastTheHour,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(24*time.Hour + 31*time.Minute),
			expectedDuration: 3*time.Hour + 59*time.Minute,
		},
		{
			name: "another complex schedule skipping weekend",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "30 10,11,12 * * 1-5",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1HalfPastTheHour,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(30*time.Hour + 30*time.Minute),
			expectedDuration: 66 * time.Hour,
		},
		{
			name: "once a week cronjob, missed two runs",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 12 * * 4",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1TwoHoursLater,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(19*24*time.Hour + 1*time.Hour + 30*time.Minute),
			expectedDuration: 48*time.Hour + 30*time.Minute,
		},
		{
			name: "no previous run of a cronjob",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 12 * * 5",
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(6 * time.Hour),
			expectedDuration: 20 * time.Hour,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := cron.ParseStandard(tt.cj.Spec.Schedule)
			if err != nil {
				t.Errorf("error setting up the test, %s", err)
			}
			gotScheduleTimeDuration := NextScheduleTimeDuration(logger, tt.cj, tt.now, sched)
			if gotScheduleTimeDuration < 0 {
				t.Errorf("scheduleTimeDuration should never be less than 0, got %s", gotScheduleTimeDuration)
			}
			if !reflect.DeepEqual(&gotScheduleTimeDuration, &tt.expectedDuration) {
				t.Errorf("scheduleTimeDuration - got %s, want %s, difference: %s", gotScheduleTimeDuration, tt.expectedDuration, gotScheduleTimeDuration-tt.expectedDuration)
			}
		})
	}
}

func TestLastScheduleTimeDuration(t *testing.T) {
	logger := testr.New(t)
	metav1TopOfTheHour := metav1.NewTime(*topOfTheHour())
	metav1OneHourAgo := metav1.NewTime(*deltaTimeAfterTopOfTheHour(-1 * time.Hour))
	metav1YesterdayMidday := metav1.NewTime(metav1TopOfTheHour.Add(-28 * time.Hour))
	metav1TwoDaysAgo := metav1.NewTime(metav1TopOfTheHour.Add(-48 * time.Hour))
	metav1FourMonthsAgo := metav1.NewTime(metav1TopOfTheHour.AddDate(0, -4, 0))
	metav1FiveMonthsAgo := metav1.NewTime(metav1TopOfTheHour.AddDate(0, -5, 0))

	tests := []struct {
		name             string
		cj               *rayv1.RayJob
		now              time.Time
		expectedDuration time.Duration
	}{
		{
			name: "hourly job, last scheduled 30 minutes ago",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 * * * *",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1TopOfTheHour,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(30 * time.Minute),
			expectedDuration: 30 * time.Minute,
		},
		{
			name: "daily job, last scheduled yesterday, now is midday next day",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TwoDaysAgo,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 12 * * *",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1YesterdayMidday,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(26*time.Hour + 5*time.Minute),
			expectedDuration: 5 * time.Minute,
		},
		{
			name: "weekly job, no previous run, now before first schedule",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 12 * * 5",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: nil,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(24 * time.Hour),
			expectedDuration: 24 * time.Hour,
		},
		{
			name: "weekly job, no previous run, now after first schedule (missed)",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 10 * * 5",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: nil,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(8 * 24 * time.Hour),
			expectedDuration: 0 * time.Minute,
		},
		{
			name: "cronjob with lastScheduleTime equal to now",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 * * * *",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1TopOfTheHour,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(0 * time.Minute),
			expectedDuration: 0 * time.Minute,
		},
		{
			name: "complex schedule, now before first next run after lastScheduleTime",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1OneHourAgo,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "30 6-16/4 * * 1-5",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: func() *metav1.Time {
						t := metav1.NewTime(*deltaTimeAfterTopOfTheHour(-90 * time.Minute))
						return &t
					}(),
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(-60 * time.Minute),
			expectedDuration: 30 * time.Minute,
		},
		{
			name: "daily job, last scheduled today earlier, now is later",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1TopOfTheHour,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 16 * * *",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1TopOfTheHour,
				},
			},
			now:              *deltaTimeAfterTopOfTheHour(1 * time.Hour),
			expectedDuration: 1 * time.Hour,
		},
		{
			name: "monthly job, missed several months, now is far past last schedule",
			cj: &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1FiveMonthsAgo,
				},
				Spec: rayv1.RayJobSpec{
					Schedule: "0 0 1 * *",
				},
				Status: rayv1.RayJobStatus{
					LastScheduleTime: &metav1FourMonthsAgo,
				},
			},
			now:              metav1TopOfTheHour.Add(1 * time.Hour),
			expectedDuration: metav1TopOfTheHour.Add(1 * time.Hour).Sub(time.Date(2016, time.May, 1, 0, 0, 0, 0, time.UTC)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := cron.ParseStandard(tt.cj.Spec.Schedule)
			if err != nil {
				t.Errorf("error setting up the test, %s", err)
				return
			}
			t.Log(tt.now)

			gotScheduleTimeDuration := LastScheduleTimeDuration(logger, tt.cj, tt.now, sched)

			if gotScheduleTimeDuration < 0 {
				t.Errorf("LastScheduleTimeDuration should never be less than 0, got %s", gotScheduleTimeDuration)
			}

			if !reflect.DeepEqual(gotScheduleTimeDuration, tt.expectedDuration) {
				t.Errorf("LastScheduleTimeDuration - got %s, want %s, difference: %s", gotScheduleTimeDuration, tt.expectedDuration, gotScheduleTimeDuration-tt.expectedDuration)
			}
		})
	}
}

func topOfTheHour() *time.Time {
	T1, err := time.Parse(time.RFC3339, "2016-05-19T10:00:00Z")
	if err != nil {
		panic("test setup error")
	}
	return &T1
}

func deltaTimeAfterTopOfTheHour(duration time.Duration) *time.Time {
	T1, err := time.Parse(time.RFC3339, "2016-05-19T10:00:00Z")
	if err != nil {
		panic("test setup error")
	}
	t := T1.Add(duration)
	return &t
}
