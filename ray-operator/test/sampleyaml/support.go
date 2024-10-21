package sampleyaml

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

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

func IsRayReady(t Test, pods []corev1.Pod) bool {
	cmd := []string{
		"python",
		"-c",
		"import ray; ray.init(); print(ray.cluster_resources())",
	}

	for _, pod := range pods {
		req := t.Client().Core().CoreV1().RESTClient().
			Post().
			Resource("pods").
			Name(pod.Name).
			Namespace(pod.Namespace).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Command: cmd,
				Stdin:   false,
				Stdout:  true,
				Stderr:  true,
				TTY:     false,
			}, clientgoscheme.ParameterCodec)

		cfg := t.Client().Config()
		exec, err := remotecommand.NewSPDYExecutor(&cfg, "POST", req.URL())
		if err != nil {
			t.T().Logf("Error creating executor for pod %s: %s", pod.Name, err)
			return false
		}

		var stdout, stderr bytes.Buffer
		err = exec.StreamWithContext(t.Ctx(), remotecommand.StreamOptions{
			Stdin:  nil,
			Stdout: &stdout,
			Stderr: &stderr,
			Tty:    false,
		})
		if err != nil {
			t.T().Logf("Error streaming command output for pod %s: %s", pod.Name, err)
			return false
		}

		if stdout.String() == "" || strings.Contains(stderr.String(), "error") {
			t.T().Logf("Ray cluster resources check failed on pod %s. stderr: %s", pod.Name, stderr.String())
			return false
		}

		t.T().Logf("Ray cluster resources from pod %s: %s", pod.Name, stdout.String())
	}

	return true
}
