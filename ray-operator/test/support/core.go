package support

import (
	"io"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
