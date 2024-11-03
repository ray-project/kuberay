package sampleyaml

import (
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func GetSampleYAMLDir(t Test) string {
	t.T().Helper()
	_, b, _, _ := runtime.Caller(0)
	sampleYAMLDir := filepath.Join(filepath.Dir(b), "../../config/samples")
	info, err := os.Stat(sampleYAMLDir)
	assert.NoError(t.T(), err)
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
			for _, container := range pod.Spec.Containers {
				ExecPodCmd(t, &pod, container.Name, cmd)
			}
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
		cfg := t.Client().Config()
		// source: ray-operator/main_test.go
		provider := configapi.Configuration{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Configuration",
				APIVersion: "config.ray.io/v1alpha1",
			},
			MetricsAddr:          ":8080",
			ProbeAddr:            ":8082",
			EnableLeaderElection: ptr.To(true),
			ReconcileConcurrency: 1,
		}
		// source: ray-operator/controllers/ray/suite_test.go
		mgr, err := ctrl.NewManager(&cfg, ctrl.Options{
			Scheme: scheme.Scheme,
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
		})
		dashboardClientFunc := GetInitedDashboardClient(t, provider, mgr, rayCluster)
		_, err = dashboardClientFunc().GetMultiApplicationStatus(t.Ctx())
		g.Expect(err).NotTo(HaveOccurred())
	}
}
