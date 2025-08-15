package e2erayjobsubmitter

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	SubmitterImage = "kuberay/submitter:nightly"
)

func TestRayJobSubmitter(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	TestScriptAC := NewConfigMap(namespace.Name, Files(test, "counter.py"))
	TestScript, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), TestScriptAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", TestScript.Namespace, TestScript.Name)
	SubmitterPodTemplate := JobSubmitterPodTemplateApplyConfiguration()
	image := SubmitterImage
	SubmitterPodTemplate.Spec.Containers[0].Image = &image
	SubmitterPodTemplate.Spec.Containers[0].Command = []string{"/submitter"}
	SubmitterPodTemplate.Spec.Containers[0].Args = []string{"--runtime-env-json", `{"pip":["requests==2.26.0","pendulum==2.1.2"],"env_vars":{"counter_name":"test_counter"}}`, "--", "python", "/home/ray/jobs/counter.py"}

	test.T().Run("Successful RayJob with light weight submitter", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).WithSpec(
			rayv1ac.RayJobSpec().
				WithRayClusterSpec(NewRayClusterSpec(MountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](TestScript, "/home/ray/jobs"))).
				WithSubmitterPodTemplate(SubmitterPodTemplate).
				WithShutdownAfterJobFinishes(true),
		)
		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		// Custom function that both checks the condition AND logs submitter job/pod info
		checkRayJobWithLogging := func() rayv1.JobDeploymentStatus {
			// Log submitter job and pod information on each poll
			logSubmitterJobAndPods(test, rayJob.Namespace, rayJob.Name)

			// Get and return the actual status
			currentRayJob, err := GetRayJob(test, rayJob.Namespace, rayJob.Name)
			if err != nil {
				LogWithTimestamp(test.T(), "Error getting RayJob: %v", err)
				return rayv1.JobDeploymentStatusInitializing
			}

			LogWithTimestamp(test.T(), "RayJob %s/%s current status: %s", rayJob.Namespace, rayJob.Name, currentRayJob.Status.JobDeploymentStatus)
			return currentRayJob.Status.JobDeploymentStatus
		}

		// Use Eventually with the custom logging function
		g.Eventually(checkRayJobWithLogging, TestTimeoutMedium).Should(Equal(rayv1.JobDeploymentStatusComplete))
	})
}

// logSubmitterJobAndPods logs information about the K8s Job and its pods created by the RayJob
func logSubmitterJobAndPods(test Test, namespace, rayJobName string) {
	test.T().Helper()

	// The job name is the same as the RayJob name (see RayJobK8sJobNamespacedName)
	jobNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      rayJobName, // K8s Job has the same name as RayJob
	}

	k8sJob, err := test.Client().Core().BatchV1().Jobs(namespace).Get(test.Ctx(), jobNamespacedName.Name, metav1.GetOptions{})
	if err != nil {
		LogWithTimestamp(test.T(), "K8s Job %s/%s not found yet: %v", namespace, rayJobName, err)
		return
	}

	// Alternative way using the common function (cleaner):
	// jobNamespacedName := common.RayJobK8sJobNamespacedName(rayJob)

	LogWithTimestamp(test.T(), "K8s Job %s/%s found - Active: %d, Succeeded: %d, Failed: %d",
		namespace, rayJobName,
		k8sJob.Status.Active,
		k8sJob.Status.Succeeded,
		k8sJob.Status.Failed)

	// Get pods created by this Kubernetes Job
	// Jobs create pods with the label "job-name=<job-name>"
	jobPods, err := test.Client().Core().CoreV1().Pods(namespace).List(test.Ctx(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", rayJobName),
	})
	if err != nil {
		LogWithTimestamp(test.T(), "Error getting pods for K8s Job %s/%s: %v", namespace, rayJobName, err)
		return
	}

	if len(jobPods.Items) == 0 {
		LogWithTimestamp(test.T(), "No pods found for K8s Job %s/%s", namespace, rayJobName)
		return
	}

	for _, pod := range jobPods.Items {
		LogWithTimestamp(test.T(), "Submitter pod %s/%s - Phase: %s, Reason: %s",
			pod.Namespace, pod.Name, pod.Status.Phase, pod.Status.Reason)

		// Log container statuses
		for _, containerStatus := range pod.Status.ContainerStatuses {
			LogWithTimestamp(test.T(), "  Container %s - Ready: %t, RestartCount: %d",
				containerStatus.Name, containerStatus.Ready, containerStatus.RestartCount)

			if containerStatus.State.Waiting != nil {
				LogWithTimestamp(test.T(), "    Waiting: %s - %s",
					containerStatus.State.Waiting.Reason, containerStatus.State.Waiting.Message)
			}
			if containerStatus.State.Terminated != nil {
				LogWithTimestamp(test.T(), "    Terminated: %s - %s (Exit code: %d)",
					containerStatus.State.Terminated.Reason,
					containerStatus.State.Terminated.Message,
					containerStatus.State.Terminated.ExitCode)
			}
			if containerStatus.State.Running != nil {
				LogWithTimestamp(test.T(), "    Running since: %s",
					containerStatus.State.Running.StartedAt.Time.Format("15:04:05"))
			}
		}

		// Get and log recent pod logs
		for _, container := range pod.Spec.Containers {
			logOptions := &corev1.PodLogOptions{
				Container: container.Name,
				TailLines: int64Ptr(20),
			}

			stream, err := test.Client().Core().CoreV1().Pods(namespace).GetLogs(pod.Name, logOptions).Stream(test.Ctx())
			if err != nil {
				LogWithTimestamp(test.T(), "Error getting logs from pod %s container %s: %v", pod.Name, container.Name, err)
				continue
			}

			buf := make([]byte, 2048)
			n, _ := stream.Read(buf)
			stream.Close()

			if n > 0 {
				LogWithTimestamp(test.T(), "Pod %s container %s recent logs:\n%s", pod.Name, container.Name, string(buf[:n]))
			} else {
				LogWithTimestamp(test.T(), "No logs available yet for pod %s container %s", pod.Name, container.Name)
			}
		}
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}
