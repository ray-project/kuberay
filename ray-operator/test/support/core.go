package support

import (
	"io"
	"os/exec"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Pods(t Test, namespace string, options ...Option[*metav1.ListOptions]) func(g gomega.Gomega) []corev1.Pod {
	return func(g gomega.Gomega) []corev1.Pod {
		listOptions := &metav1.ListOptions{}

		for _, option := range options {
			t.Expect(option.applyTo(listOptions)).To(gomega.Succeed())
		}

		pods, err := t.Client().Core().CoreV1().Pods(namespace).List(t.Ctx(), *listOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return pods.Items
	}
}

func storeAllPodLogs(t Test, namespace *corev1.Namespace) {
	t.T().Helper()

	pods, err := t.Client().Core().CoreV1().Pods(namespace.Name).List(t.Ctx(), metav1.ListOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			t.T().Logf("Retrieving Pod Container %s/%s/%s logs", pod.Namespace, pod.Name, container.Name)
			storeContainerLog(t, namespace, pod.Name, container.Name)
		}
	}
}

func storeContainerLog(t Test, namespace *corev1.Namespace, podName, containerName string) {
	t.T().Helper()

	options := corev1.PodLogOptions{Container: containerName}
	stream, err := t.Client().Core().CoreV1().Pods(namespace.Name).GetLogs(podName, &options).Stream(t.Ctx())
	if err != nil {
		t.T().Logf("Error getting logs from container %s/%s/%s", namespace.Name, podName, containerName)
		return
	}
	t.Expect(err).NotTo(gomega.HaveOccurred())

	defer func() {
		t.Expect(stream.Close()).To(gomega.Succeed())
	}()

	bytes, err := io.ReadAll(stream)
	t.Expect(err).NotTo(gomega.HaveOccurred())

	containerLogFileName := "pod-" + podName + "-" + containerName
	WriteToOutputDir(t, containerLogFileName, Log, bytes)
}

func ExecPodCmd(t Test, pod *corev1.Pod, containerName string, cmd []string) {
	kubectlCmd := []string{"exec", pod.Name, "-n", pod.Namespace, "-c", containerName, "--"}
	kubectlCmd = append(kubectlCmd, cmd...)

	t.T().Logf("Executing command: kubectl %s", kubectlCmd)
	output, err := exec.Command("kubectl", kubectlCmd...).CombinedOutput()
	t.T().Logf("Command output: %s", output)
	t.Expect(err).NotTo(gomega.HaveOccurred())
}
