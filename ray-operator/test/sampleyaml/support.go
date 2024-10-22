package sampleyaml

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayscheme "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func getSampleYAMLDir(t Test) string {
	t.T().Helper()
	_, b, _, _ := runtime.Caller(0)
	sampleYAMLDir := filepath.Join(filepath.Dir(b), "../../config/samples")
	info, err := os.Stat(sampleYAMLDir)
	t.Expect(err).NotTo(gomega.HaveOccurred())
	t.Expect(info.IsDir()).To(gomega.BeTrue())
	return sampleYAMLDir
}

func readYAML(t Test, filename string) []byte {
	t.T().Helper()
	sampleYAMLDir := getSampleYAMLDir(t)
	yamlFile := filepath.Join(sampleYAMLDir, filename)
	yamlFileContent, err := os.ReadFile(yamlFile)
	t.Expect(err).NotTo(gomega.HaveOccurred())
	return yamlFileContent
}

func DeserializeRayClusterSampleYAML(t Test, filename string) *rayv1.RayCluster {
	t.T().Helper()
	yamlFileContent := readYAML(t, filename)
	decoder := rayscheme.Codecs.UniversalDecoder()
	rayCluster := &rayv1.RayCluster{}
	_, _, err := decoder.Decode(yamlFileContent, nil, rayCluster)
	t.Expect(err).NotTo(gomega.HaveOccurred())
	return rayCluster
}

func KubectlApplyYAML(t Test, filename string, namespace string) {
	t.T().Helper()
	sampleYAMLDir := getSampleYAMLDir(t)
	sampleYAMLPath := filepath.Join(sampleYAMLDir, filename)
	kubectlCmd := exec.CommandContext(t.Ctx(), "kubectl", "apply", "-f", sampleYAMLPath, "-n", namespace)
	err := kubectlCmd.Run()
	t.Expect(err).NotTo(gomega.HaveOccurred())
	t.T().Logf("Successfully applied %s", filename)
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

func SubmitJobsToAllPods(t Test, pods []corev1.Pod) func(g gomega.Gomega) {
	return func(g gomega.Gomega) {
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
