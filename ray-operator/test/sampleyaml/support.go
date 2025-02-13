package sampleyaml

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types" // needed for GomegaTestingT
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func GetSampleYAMLDir(t Test) string {
	t.T().Helper()
	_, b, _, _ := runtime.Caller(0)
	sampleYAMLDir := filepath.Join(filepath.Dir(b), "../../config/samples")
	info, err := os.Stat(sampleYAMLDir)
	require.NoError(t.T(), err)
	assert.True(t.T(), info.IsDir())
	return sampleYAMLDir
}

func IsPodRunningAndReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func AllPodsRunningAndReady(pods []corev1.Pod) bool {
	for _, pod := range pods {
		if !IsPodRunningAndReady(&pod) {
			return false
		}
	}
	return true
}

func SubmitJobsToAllPods(t Test, rayCluster *rayv1.RayCluster) func(Gomega) {
	return func(g Gomega) {
		pods, err := GetAllPods(t, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		cmd := []string{
			"python",
			"-c",
			"import ray; ray.init(); print(ray.cluster_resources())",
		}
		for _, pod := range pods {
			container := pod.Spec.Containers[utils.RayContainerIndex] // Directly access the Ray container
			ExecPodCmd(t, &pod, container.Name, cmd)
		}
	}
}

func getApps(rayService *rayv1.RayService) map[string]rayv1.AppStatus {
	apps := make(map[string]rayv1.AppStatus)
	for k, v := range rayService.Status.ActiveServiceStatus.Applications {
		apps[k] = v
	}
	return apps
}

func AllAppsRunning(rayService *rayv1.RayService) bool {
	appStatuses := getApps(rayService)
	if len(appStatuses) == 0 {
		return false
	}

	for _, appStatus := range appStatuses {
		if appStatus.Status != rayv1.ApplicationStatusEnum.RUNNING {
			return false
		}
	}
	return true
}

func QueryDashboardGetAppStatus(t Test, rayCluster *rayv1.RayCluster) func(Gomega) {
	return func(g Gomega) {
		rayDashboardClient := &utils.RayDashboardClient{}
		pod, err := GetHeadPod(t, rayCluster)
		g.Expect(err).ToNot(HaveOccurred())

		localPort := 8265
		remotePort := 8265
		stopChan, err := SetupPortForward(t, pod.Name, pod.Namespace, localPort, remotePort)
		defer close(stopChan)

		g.Expect(err).ToNot(HaveOccurred())
		url := fmt.Sprintf("127.0.0.1:%d", localPort)

		err = rayDashboardClient.InitClient(t.Ctx(), url, rayCluster)
		g.Expect(err).ToNot(HaveOccurred())
		serveDetails, err := rayDashboardClient.GetServeDetails(t.Ctx())
		g.Expect(err).ToNot(HaveOccurred())

		for _, value := range serveDetails.Applications {
			g.Expect(value.ServeApplicationStatus.Status).To(Equal(rayv1.ApplicationStatusEnum.RUNNING))
		}
	}
}

func WithRayJobResourceLogger(t Test) types.GomegaTestingT {
	return &RayJobResourceLogger{t: t}
}

type RayJobResourceLogger struct {
	t Test
}

func (l *RayJobResourceLogger) Helper() {
	l.t.T().Helper()
}

func (l *RayJobResourceLogger) Fatalf(format string, args ...interface{}) {
	l.Helper()
	var sb strings.Builder

	// Log the original failure message
	fmt.Fprintf(&sb, format, args...)

	if pods, err := l.t.Client().Core().CoreV1().Pods("").List(l.t.Ctx(), metav1.ListOptions{}); err == nil {
		fmt.Fprintf(&sb, "\n=== Pods across all namespaces ===\n")
		for _, pod := range pods.Items {
			podJSON, err := json.MarshalIndent(pod, "", "    ")
			if err != nil {
				fmt.Fprintf(&sb, "Error marshaling pod %s/%s: %v\n", pod.Namespace, pod.Name, err)
				continue
			}
			fmt.Fprintf(&sb, "---\n# Pod: %s/%s\n%s\n", pod.Namespace, pod.Name, string(podJSON))
		}
	} else {
		fmt.Fprintf(&sb, "Failed to get pods: %v\n", err)
	}

	if jobs, err := l.t.Client().Core().BatchV1().Jobs("").List(l.t.Ctx(), metav1.ListOptions{}); err == nil {
		fmt.Fprintf(&sb, "\n=== Jobs across all namespaces ===\n")
		for _, job := range jobs.Items {
			jobJSON, err := json.MarshalIndent(job, "", "    ")
			if err != nil {
				fmt.Fprintf(&sb, "Error marshaling job %s/%s: %v\n", job.Namespace, job.Name, err)
				continue
			}
			fmt.Fprintf(&sb, "---\n# Job: %s/%s\n%s\n", job.Namespace, job.Name, string(jobJSON))
		}
	} else {
		fmt.Fprintf(&sb, "Failed to get jobs: %v\n", err)
	}

	if services, err := l.t.Client().Core().CoreV1().Services("").List(l.t.Ctx(), metav1.ListOptions{}); err == nil {
		fmt.Fprintf(&sb, "\n=== Services across all namespaces ===\n")
		for _, svc := range services.Items {
			serviceJSON, err := json.MarshalIndent(svc, "", "    ")
			if err != nil {
				fmt.Fprintf(&sb, "Error marshaling service %s/%s: %v\n", svc.Namespace, svc.Name, err)
				continue
			}
			fmt.Fprintf(&sb, "---\n# Service: %s/%s\n%s\n", svc.Namespace, svc.Name, string(serviceJSON))
		}
	} else {
		fmt.Fprintf(&sb, "Failed to get services: %v\n", err)
	}

	if rayJobs, err := l.t.Client().Ray().RayV1().RayJobs("").List(l.t.Ctx(), metav1.ListOptions{}); err == nil {
		fmt.Fprintf(&sb, "\n=== RayJobs across all namespaces ===\n")
		for _, rayJob := range rayJobs.Items {
			rayJobJSON, err := json.MarshalIndent(rayJob, "", "    ")
			if err != nil {
				fmt.Fprintf(&sb, "Error marshaling rayjob %s/%s: %v\n", rayJob.Namespace, rayJob.Name, err)
				continue
			}
			fmt.Fprintf(&sb, "---\n# RayJob: %s/%s\n%s\n", rayJob.Namespace, rayJob.Name, string(rayJobJSON))
		}
	} else {
		fmt.Fprintf(&sb, "Failed to get rayjobs: %v\n", err)
	}

	l.t.T().Fatal(sb.String())
}
