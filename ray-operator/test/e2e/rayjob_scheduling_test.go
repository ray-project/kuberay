package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobScheduling(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	jobsAC := newConfigMap(namespace.Name, files(test, "counter.py", "fail.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), jobsAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	test.T().Run("Sucessful RayJob scheduling WITHOUT deleting cluster after each job", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`
									env_vars:
									counter_name: test_counter
								`).
				WithShutdownAfterJobFinishes(true).
				WithSchedule("*/3 * * * *").
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob with scheduling %s/%s successfully", rayJob.Namespace, rayJob.Name)

		numExpectedRuns := int32(3)
		LogWithTimestamp(test.T(), "Waiting for %d successful runs of RayJob %s/%s", numExpectedRuns, rayJob.Namespace, rayJob.Name)

		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutLong*2). // Give it a generous timeout (e.g., 2-3 minutes)
												Should(WithTransform(RayJobSucceeded, Equal(numExpectedRuns)))

		expectedCompleteCounts := 3
		var lastScheduleTime *metav1.Time
		for i := 0; i < expectedCompleteCounts; i++ {
			LogWithTimestamp(test.T(), "--- Verifying Cycle %d: Waiting for JobDeploymentStatus %s ---", i+1, rayv1.JobDeploymentStatusComplete)

			// 1. Wait until the job reaches 'JobDeploymentStatusComplete' for the current cycle
			g.Eventually(RayCluster(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
				Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

			// 2. Get the latest RayJob instance to check its status fields
			currentRayJob, err := GetRayJob(test, rayJob.Namespace, rayJob.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(currentRayJob.Status.LastScheduleTime).NotTo(BeNil()) // Ensure LastScheduleTime is populated

			// 3. Verify LastScheduleTime advancement (if not the very first cycle)
			if lastScheduleTime != nil {
				durationSinceLastScheduled := currentRayJob.Status.LastScheduleTime.Time.Sub(lastScheduleTime.Time)
				LogWithTimestamp(test.T(), "Observed LastScheduleTime advanced by: %v (expected ~1m)", durationSinceLastScheduled)
				// Allow a buffer for timing variations (e.g., 1 minute +/- 15 seconds)
				g.Expect(durationSinceLastScheduled).To(BeNumerically("~", 1*time.Minute, 15*time.Second))
			}
			// Update lastScheduleTime for the next iteration
			lastScheduleTime = currentRayJob.Status.LastScheduleTime

			// 4. If more cycles are expected, wait for the status to reset (from Complete to New/Scheduled)
			// This is crucial for the next g.Eventually to "see" a state change from 'New'/'Scheduled' back to 'Complete'.
			if i < expectedCompleteCounts-1 {
				LogWithTimestamp(test.T(), "Waiting for JobDeploymentStatus to reset for next cycle %d", i+2)
				g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutShort). // Should be a quick transition
														Should(WithTransform(RayJobDeploymentStatus, SatisfyAny(
						Equal(rayv1.JobDeploymentStatusNew),
						Equal(rayv1.JobDeploymentStatusScheduled),
					)))
			}

		}

		LogWithTimestamp(test.T(), "Successfully observed RayJob %s/%s reach %s %d times.", rayJob.Name, rayJob.Namespace, rayv1.JobDeploymentStatusComplete, expectedCompleteCounts)
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

	})

	test.T().Run("Sucessful RayJob scheduling WITH deleting cluster after each job", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`
									env_vars:
									counter_name: test_counter
								`).
				WithShutdownAfterJobFinishes(true).
				WithSchedule("*/3 * * * *").
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob with scheduling %s/%s successfully", rayJob.Namespace, rayJob.Name)

		numExpectedRuns := int32(3)
		LogWithTimestamp(test.T(), "Waiting for %d successful runs of RayJob %s/%s", numExpectedRuns, rayJob.Namespace, rayJob.Name)

		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutLong*2). // Give it a generous timeout (e.g., 2-3 minutes)
												Should(WithTransform(RayJobSucceeded, Equal(numExpectedRuns)))

		expectedCompleteCounts := 3
		var lastScheduleTime *metav1.Time
		for i := 0; i < expectedCompleteCounts; i++ {
			LogWithTimestamp(test.T(), "--- Verifying Cycle %d: Waiting for JobDeploymentStatus %s ---", i+1, rayv1.JobDeploymentStatusComplete)

			// 1. Wait until the job reaches 'JobDeploymentStatusComplete' for the current cycle
			g.Eventually(RayCluster(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
				Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

			// 2. Get the latest RayJob instance to check its status fields
			currentRayJob, err := GetRayJob(test, rayJob.Namespace, rayJob.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(currentRayJob.Status.LastScheduleTime).NotTo(BeNil()) // Ensure LastScheduleTime is populated

			// 3. Verify LastScheduleTime advancement (if not the very first cycle)
			if lastScheduleTime != nil {
				durationSinceLastScheduled := currentRayJob.Status.LastScheduleTime.Time.Sub(lastScheduleTime.Time)
				LogWithTimestamp(test.T(), "Observed LastScheduleTime advanced by: %v (expected ~1m)", durationSinceLastScheduled)
				// Allow a buffer for timing variations (e.g., 1 minute +/- 15 seconds)
				g.Expect(durationSinceLastScheduled).To(BeNumerically("~", 1*time.Minute, 15*time.Second))
			}
			// Update lastScheduleTime for the next iteration
			lastScheduleTime = currentRayJob.Status.LastScheduleTime

			// 4. If more cycles are expected, wait for the status to reset (from Complete to New/Scheduled)
			// This is crucial for the next g.Eventually to "see" a state change from 'New'/'Scheduled' back to 'Complete'.
			if i < expectedCompleteCounts-1 {
				LogWithTimestamp(test.T(), "Waiting for JobDeploymentStatus to reset for next cycle %d", i+2)
				g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutShort). // Should be a quick transition
														Should(WithTransform(RayJobDeploymentStatus, SatisfyAny(
						Equal(rayv1.JobDeploymentStatusNew),
						Equal(rayv1.JobDeploymentStatusScheduled),
					)))
			}

		}

		LogWithTimestamp(test.T(), "Successfully observed RayJob %s/%s reach %s %d times.", rayJob.Name, rayJob.Namespace, rayv1.JobDeploymentStatusComplete, expectedCompleteCounts)
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

	})

	test.T().Run("Overlapping RayJobs", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/long_running_counter.py").
				WithRuntimeEnvYAML(`
									env_vars:
									counter_name: test_counter
								`).
				WithShutdownAfterJobFinishes(false).
				WithSchedule("*/1 * * * *").
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob with scheduling %s/%s successfully", rayJob.Namespace, rayJob.Name)

		numExpectedRuns := int32(3)
		LogWithTimestamp(test.T(), "Waiting for %d successful runs of RayJob %s/%s", numExpectedRuns, rayJob.Namespace, rayJob.Name)

		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutLong*2). // Give it a generous timeout (e.g., 2-3 minutes)
												Should(WithTransform(RayJobSucceeded, Equal(numExpectedRuns)))

		expectedCompleteCounts := 3
		var lastScheduleTime *metav1.Time
		for i := 0; i < expectedCompleteCounts; i++ {
			LogWithTimestamp(test.T(), "--- Verifying Cycle %d: Waiting for JobDeploymentStatus %s ---", i+1, rayv1.JobDeploymentStatusComplete)

			// 1. Wait until the job reaches 'JobDeploymentStatusComplete' for the current cycle
			g.Eventually(RayCluster(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
				Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

			// 2. Get the latest RayJob instance to check its status fields
			currentRayJob, err := GetRayJob(test, rayJob.Namespace, rayJob.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(currentRayJob.Status.LastScheduleTime).NotTo(BeNil()) // Ensure LastScheduleTime is populated

			// 3. Verify LastScheduleTime advancement (if not the very first cycle)
			if lastScheduleTime != nil {
				durationSinceLastScheduled := currentRayJob.Status.LastScheduleTime.Time.Sub(lastScheduleTime.Time)
				LogWithTimestamp(test.T(), "Observed LastScheduleTime advanced by: %v (expected ~1m)", durationSinceLastScheduled)
				// Allow a buffer for timing variations (e.g., 1 minute +/- 15 seconds)
				g.Expect(durationSinceLastScheduled).To(BeNumerically("~", 1*time.Minute, 15*time.Second))
			}
			// Update lastScheduleTime for the next iteration
			lastScheduleTime = currentRayJob.Status.LastScheduleTime

			// 4. If more cycles are expected, wait for the status to reset (from Complete to New/Scheduled)
			// This is crucial for the next g.Eventually to "see" a state change from 'New'/'Scheduled' back to 'Complete'.
			if i < expectedCompleteCounts-1 {
				LogWithTimestamp(test.T(), "Waiting for JobDeploymentStatus to reset for next cycle %d", i+2)
				g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutShort). // Should be a quick transition
														Should(WithTransform(RayJobDeploymentStatus, SatisfyAny(
						Equal(rayv1.JobDeploymentStatusNew),
						Equal(rayv1.JobDeploymentStatusScheduled),
					)))
			}

		}

		LogWithTimestamp(test.T(), "Successfully observed RayJob %s/%s reach %s %d times.", rayJob.Name, rayJob.Namespace, rayv1.JobDeploymentStatusComplete, expectedCompleteCounts)
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

	})

	test.T().Run("Bad Cron String", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/long_running_counter.py").
				WithRuntimeEnvYAML(`
									env_vars:
									counter_name: test_counter
								`).
				WithShutdownAfterJobFinishes(false).
				WithSchedule("*(12*12)qw").
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob with scheduling %s/%s successfully", rayJob.Namespace, rayJob.Name)

		numExpectedRuns := int32(3)
		LogWithTimestamp(test.T(), "Waiting for %d successful runs of RayJob %s/%s", numExpectedRuns, rayJob.Namespace, rayJob.Name)

		// `shedule` is not a valid cron string, so the RayJob controller will not do anything with the CR.
		g.Consistently(RayJob(test, rayJob.Namespace, rayJob.Name), 5*time.Second).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusFailed)))

	})

}
