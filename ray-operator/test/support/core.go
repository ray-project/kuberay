package support

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

func Pods(t Test, namespace string, options ...Option[*metav1.ListOptions]) func(g gomega.Gomega) []corev1.Pod {
	return func(g gomega.Gomega) []corev1.Pod {
		listOptions := &metav1.ListOptions{}

		for _, option := range options {
			g.Expect(option.applyTo(listOptions)).To(gomega.Succeed())
		}

		pods, err := t.Client().Core().CoreV1().Pods(namespace).List(t.Ctx(), *listOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return pods.Items
	}
}

func storeAllPodLogs(t Test, namespace *corev1.Namespace) {
	t.T().Helper()

	pods, err := t.Client().Core().CoreV1().Pods(namespace.Name).List(t.Ctx(), metav1.ListOptions{})
	require.NoError(t.T(), err)

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			LogWithTimestamp(t.T(), "Retrieving Pod Container %s/%s/%s logs", pod.Namespace, pod.Name, container.Name)
			storeContainerLog(t, namespace, pod.Name, container.Name)
		}
	}
}

func storeContainerLog(t Test, namespace *corev1.Namespace, podName, containerName string) {
	t.T().Helper()

	options := corev1.PodLogOptions{Container: containerName}
	stream, err := t.Client().Core().CoreV1().Pods(namespace.Name).GetLogs(podName, &options).Stream(t.Ctx())
	if err != nil {
		LogWithTimestamp(t.T(), "Error getting logs from container %s/%s/%s", namespace.Name, podName, containerName)
		return
	}
	require.NoError(t.T(), err)

	defer func() {
		require.NoError(t.T(), stream.Close())
	}()

	bytes, err := io.ReadAll(stream)
	require.NoError(t.T(), err)

	containerLogFileName := "pod-" + podName + "-" + containerName
	WriteToOutputDir(t, containerLogFileName, Log, bytes)
}

func ExecPodCmd(t Test, pod *corev1.Pod, containerName string, cmd []string) (bytes.Buffer, bytes.Buffer) {
	req := t.Client().Core().CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   cmd,
			Container: containerName,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, clientgoscheme.ParameterCodec)

	LogWithTimestamp(t.T(), "Executing command: %s", cmd)
	cfg := t.Client().Config()
	exec, err := remotecommand.NewSPDYExecutor(&cfg, "POST", req.URL())
	require.NoError(t.T(), err)
	// Capture the output streams
	var stdout, stderr bytes.Buffer
	// Execute the command in the pod
	err = exec.StreamWithContext(t.Ctx(), remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})
	LogWithTimestamp(t.T(), "Command stdout: %s", stdout.String())
	LogWithTimestamp(t.T(), "Command stderr: %s", stderr.String())
	require.NoError(t.T(), err)
	return stdout, stderr
}

func SetupPortForward(t Test, podName, namespace string, localPort, remotePort int) (chan struct{}, error) {
	cfg := t.Client().Config()

	req := t.Client().Core().CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(&cfg)
	if err != nil {
		return nil, err
	}

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{}, 1)
	out := new(strings.Builder)
	errOut := new(strings.Builder)

	// create port forward
	forwarder, err := portforward.New(
		spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL()),
		[]string{fmt.Sprintf("%d:%d", localPort, remotePort)},
		stopChan,
		readyChan,
		out,
		errOut,
	)
	if err != nil {
		return nil, err
	}

	// launch Port Forward
	go func() {
		defer GinkgoRecover()
		err := forwarder.ForwardPorts()
		require.NoError(t.T(), err)
	}()
	<-readyChan // wait for port forward to finish

	return stopChan, nil
}

func CreateCurlPod(t Test, podName, containerName, namespace string) (*corev1.Pod, error) {
	// Define the podSpec spec
	podSpec := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    containerName,
					Image:   "rancher/curl",
					Command: []string{"/bin/sh", "-c", "tail -f /dev/null"},
				},
			},
		},
	}
	return t.Client().Core().CoreV1().Pods(namespace).Create(t.Ctx(), podSpec, metav1.CreateOptions{})
}
