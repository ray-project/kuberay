/*
Portions of this file are derived from the CronJob reasource in the Kubernetes project,
Kubernetes project: https://github.com/kubernetes/kubernetes
Licensed under the Apache License, Version 2.0.
For more information on Kubernetes CronJob, refer to:
https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/
*/

package utils

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

type MissedSchedulesType int

const (
	noneMissed MissedSchedulesType = iota
	fewMissed
	manyMissed
)

func MostRecentScheduleTime(rj *rayv1.RayCronJob, now time.Time, schedule cron.Schedule) (time.Time, *time.Time, MissedSchedulesType, error) {
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

func FormatSchedule(rj *rayv1.RayCronJob, recorder record.EventRecorder) string {
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
func NextScheduleTimeDuration(logger logr.Logger, rj *rayv1.RayCronJob, now time.Time, schedule cron.Schedule) time.Duration {
	earliestTime, mostRecentTime, missedSchedules, err := MostRecentScheduleTime(rj, now, schedule)
	if err != nil {
		// we still have to requeue at some point, so aim for the next scheduling slot from now
		logger.Info("Error in mostRecentScheduleTime, we still have to requeue at some point, so aim for the next scheduling slot from now", "Error", err)
		mostRecentTime = &now
	} else if mostRecentTime == nil {
		logger.Info("mostRecentTime doesnt exist")
		if missedSchedules == noneMissed {
			// no missed schedules since earliestTime
			mostRecentTime = &earliestTime
		} else {
			// if there are missed schedules since earliestTime, always use now
			mostRecentTime = &now
		}
	}
	logger.Info("Successfully calculated earliestTime and mostRecentTime", "mostRecentTime", mostRecentTime, "Current Time", now, "Next time to aim for", schedule.Next(*mostRecentTime))
	t := schedule.Next(*mostRecentTime).Sub(now)
	return t
}

// The LastScheduleTimeDuration function returns the last previous cron time.
// It calculates the most recent time a schedule should have executed based
// on the RayJob's creation time (or its last scheduled status) and the current time 'now'.
func LastScheduleTimeDuration(logger logr.Logger, rj *rayv1.RayCronJob, now time.Time, schedule cron.Schedule) time.Duration {
	earliestTime, mostRecentTime, missedSchedules, err := MostRecentScheduleTime(rj, now, schedule)
	if err != nil {
		// We still have to requeue at some point, so aim for the next scheduling slot from now
		logger.Info("Error in mostRecentScheduleTime, we still have to requeue at some point", "Error", err)
		mostRecentTime = &now
	} else if mostRecentTime == nil {
		logger.Info("mostRecentTime doesnt exist")
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

// nextScheduleTime returns the time.Time of the next schedule after the last scheduled
// and before now, or nil if no unmet schedule times, and an error.
// If there are too many (>100) unstarted times, it will also record a warning.
func NextScheduleTime(logger logr.Logger, cj *rayv1.RayCronJob, now time.Time, schedule cron.Schedule, recorder record.EventRecorder) (*time.Time, error) {
	_, mostRecentTime, missedSchedules, err := MostRecentScheduleTime(cj, now, schedule)

	if mostRecentTime == nil || mostRecentTime.After(now) {
		return nil, err
	}

	if missedSchedules == manyMissed {
		recorder.Eventf(cj, corev1.EventTypeWarning, "TooManyMissedTimes", "too many missed start times. Set or decrease .spec.startingDeadlineSeconds or check clock skew")
		logger.Info("too many missed times")
	}

	return mostRecentTime, err
}

// getJobFromTemplate2 makes a Job from a CronJob. It converts the unix time into minutes from
// epoch time and concatenates that to the job name, because the cronjob_controller v2 has the lowest
// granularity of 1 minute for scheduling job.
func GetRayJobFromTemplate2(cj *rayv1.RayCronJob, scheduledTime time.Time) (*rayv1.RayJob, error) {
	labels := copyLabels(&cj.Spec.RayJobTemplate)
	annotations := copyAnnotations(&cj.Spec.RayJobTemplate)
	// We want job names for a given nominal start time to have a deterministic name to avoid the same job being created twice
	name := getJobName(cj, scheduledTime)

	job := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Labels:            labels,
			Annotations:       annotations,
			Name:              name,
			CreationTimestamp: metav1.Time{Time: scheduledTime},
			OwnerReferences:   []metav1.OwnerReference{*metav1.NewControllerRef(cj, rayv1.SchemeGroupVersion.WithKind("RayCronJob"))},
		},
	}
	cj.Spec.RayJobTemplate.Spec.DeepCopyInto(&job.Spec)
	job.Namespace = cj.Namespace
	return job, nil
}

func getJobName(cj *rayv1.RayCronJob, scheduledTime time.Time) string {
	return fmt.Sprintf("%s-%s-%d", "rayjob", cj.Name, getTimeHashInMinutes(scheduledTime))
}

func copyLabels(template *rayv1.RayJobTemplate) labels.Set {
	l := make(labels.Set)
	for k, v := range template.Labels {
		l[k] = v
	}
	return l
}

func copyAnnotations(template *rayv1.RayJobTemplate) labels.Set {
	a := make(labels.Set)
	for k, v := range template.Annotations {
		a[k] = v
	}
	return a
}

// getTimeHashInMinutes returns Unix Epoch Time in minutes
func getTimeHashInMinutes(scheduledTime time.Time) int64 {
	return scheduledTime.Unix() / 60
}

func DeleteFromActiveList(cj *rayv1.RayCronJob, uid types.UID) {
	if cj == nil {
		return
	}
	// TODO: @alpatel the memory footprint can may be reduced here by
	//  cj.Status.Active = append(cj.Status.Active[:indexToRemove], cj.Status.Active[indexToRemove:]...)
	newActive := []corev1.ObjectReference{}
	for _, j := range cj.Status.Active {
		if j.UID != uid {
			newActive = append(newActive, j)
		}
	}
	cj.Status.Active = newActive
}
