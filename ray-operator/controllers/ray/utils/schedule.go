package utils

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
)

type missedSchedulesType int

const (
	noneMissed missedSchedulesType = iota
	fewMissed
	manyMissed
)

func mostRecentScheduleTime(rj *rayv1.RayJob, now time.Time, schedule cron.Schedule) (time.Time, *time.Time, missedSchedulesType, error) {
	earliestTime := rj.ObjectMeta.CreationTimestamp.Time
	missedSchedules := noneMissed
	if rj.Status.LastScheduleTime != nil {
		earliestTime = rj.Status.LastScheduleTime.Time
	}

	t1 := schedule.Next(earliestTime)
	t2 := schedule.Next(t1)

	if now.Before(t1) {
		return earliestTime, nil, missedSchedules, nil
	}
	if now.Before(t2) {
		return earliestTime, &t1, missedSchedules, nil
	}

	// It is possible for cron.ParseStandard("59 23 31 2 *") to return an invalid schedule
	// minute - 59, hour - 23, dom - 31, month - 2, and dow is optional, clearly 31 is invalid
	// In this case the timeBetweenTwoSchedules will be 0, and we error out the invalid schedule
	timeBetweenTwoSchedules := int64(t2.Sub(t1).Round(time.Second).Seconds())
	if timeBetweenTwoSchedules < 1 {
		return earliestTime, nil, missedSchedules, fmt.Errorf("time difference between two schedules is less than 1 second")
	}
	// this logic used for calculating number of missed schedules does a rough
	// approximation, by calculating a diff between two schedules (t1 and t2),
	// and counting how many of these will fit in between last schedule and now
	timeElapsed := int64(now.Sub(t1).Seconds())
	numberOfMissedSchedules := (timeElapsed / timeBetweenTwoSchedules) + 1

	var mostRecentTime time.Time
	// to get the most recent time accurate for regular schedules and the ones
	// specified with @every form, we first need to calculate the potential earliest
	// time by multiplying the initial number of missed schedules by its interval,
	// this is critical to ensure @every starts at the correct time, this explains
	// the numberOfMissedSchedules-1, the additional -1 serves there to go back
	// in time one more time unit, and let the cron library calculate a proper
	// schedule, for case where the schedule is not consistent, for example
	// something like  30 6-16/4 * * 1-5
	potentialEarliest := t1.Add(time.Duration((numberOfMissedSchedules-1-1)*timeBetweenTwoSchedules) * time.Second)
	for t := schedule.Next(potentialEarliest); !t.After(now); t = schedule.Next(t) {
		mostRecentTime = t
	}

	// An object might miss several starts. For example, if
	// controller gets wedged on friday at 5:01pm when everyone has
	// gone home, and someone comes in on tuesday AM and discovers
	// the problem and restarts the controller, then all the hourly
	// jobs, more than 80 of them for one hourly cronJob, should
	// all start running with no further intervention (if the cronJob
	// allows concurrency and late starts).
	//
	// However, if there is a bug somewhere, or incorrect clock
	// on controller's server or apiservers (for setting creationTimestamp)
	// then there could be so many missed start times (it could be off
	// by decades or more), that it would eat up all the CPU and memory
	// of this controller. In that case, we want to not try to list
	// all the missed start times.
	//
	// I've somewhat arbitrarily picked 100, as more than 80,
	// but less than "lots".
	switch {
	case numberOfMissedSchedules > 100:
		missedSchedules = manyMissed
	// inform about few missed, still
	case numberOfMissedSchedules > 0:
		missedSchedules = fewMissed
	}

	if mostRecentTime.IsZero() {
		return earliestTime, nil, missedSchedules, nil
	}
	return earliestTime, &mostRecentTime, missedSchedules, nil
}

func FormatSchedule(rj *rayv1.RayJob, recorder record.EventRecorder) string {
	if strings.Contains(rj.Spec.Schedule, "TZ") {
		if recorder != nil {
			recorder.Eventf(rj, corev1.EventTypeWarning, "UnsupportedSchedule", "CRON_TZ or TZ used in schedule %q is not officially supported, see https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/ for more details", rj.Spec.Schedule)
		}

		return rj.Spec.Schedule
	}

	return rj.Spec.Schedule
}

// nextScheduleTimeDuration returns the time duration to requeue a cron schedule based on
// the schedule and last schedule time.
func NextScheduleTimeDuration(logger logr.Logger, rj *rayv1.RayJob, now time.Time, schedule cron.Schedule) time.Duration {

	earliestTime, mostRecentTime, missedSchedules, err := mostRecentScheduleTime(rj, now, schedule)
	if err != nil {
		// we still have to requeue at some point, so aim for the next scheduling slot from now
		logger.Info("Error in mostRecentScheduleTime, we still have to requeue at some point, so aim for the next scheduling slot from now", "Error", err)
		mostRecentTime = &now
	} else if mostRecentTime == nil {
		logger.Info("mostRecentTime doesnt exist", "mostRecentTime", mostRecentTime, "earliestTime", earliestTime)
		if missedSchedules == noneMissed {
			// no missed schedules since earliestTime
			mostRecentTime = &earliestTime
		} else {
			// if there are missed schedules since earliestTime, always use now
			mostRecentTime = &now
		}
	}
	logger.Info("Successfully calculated earliestTime and mostRecentTime", "mostRecentTime", mostRecentTime, "earliestTime", earliestTime, "Next time to aim for", schedule.Next(*mostRecentTime))
	t := schedule.Next(*mostRecentTime).Sub(now)
	return t
}

// The LastScheduleTimeDuration function returns the last previous cron time.
// It calculates the most recent time a schedule should have executed based
// on the RayJob's creation time (or its last scheduled status) and the current time 'now'.
func LastScheduleTimeDuration(logger logr.Logger, rj *rayv1.RayJob, now time.Time, schedule cron.Schedule) time.Duration {

	earliestTime, mostRecentTime, missedSchedules, err := mostRecentScheduleTime(rj, now, schedule)
	if err != nil {
		// We still have to requeue at some point, so aim for the next scheduling slot from now
		logger.Info("Error in mostRecentScheduleTime, we still have to requeue at some point", "Error", err)
		mostRecentTime = &now
	} else if mostRecentTime == nil {
		logger.Info("mostRecentTime doesnt exist", "mostRecentTime", mostRecentTime, "earliestTime", earliestTime)
		if missedSchedules == noneMissed {
			// No missed schedules since earliestTime
			mostRecentTime = &earliestTime
		} else {
			// If there are missed schedules since earliestTime, always use now
			mostRecentTime = &now
		}
	}
	return now.Sub(*mostRecentTime)
}
