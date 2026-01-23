package support

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os/exec"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// CreateHTTPClientWithCookieJar creates an HTTP client with cookie jar to maintain session.
func CreateHTTPClientWithCookieJar(g *WithT) *http.Client {
	jar, err := cookiejar.New(nil)
	g.Expect(err).NotTo(HaveOccurred())
	return &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}
}

// GetContainerStatusByName retrieves the container status by container name.
// NOTE: ContainerStatuses order doesn't guarantee to match Spec.Containers order.
// For more details, please refer to the following link:
// https://github.com/ray-project/kuberay/blob/7791a8786861818f0cebcce381ef221436a0fa4d/ray-operator/controllers/ray/raycluster_controller.go#L1160C1-L1171C2
func GetContainerStatusByName(pod *corev1.Pod, containerName string) (*corev1.ContainerStatus, error) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == containerName {
			return &containerStatus, nil
		}
	}
	return nil, fmt.Errorf("container %s not found in pod %s/%s", containerName, pod.Namespace, pod.Name)
}

func PortForwardService(test Test, g *WithT, namespace, serviceName string, port int) {
	ctx, cancel := context.WithCancel(context.Background())
	test.T().Cleanup(cancel)

	kubectlCmd := exec.CommandContext(
		ctx,
		"kubectl",
		"-n", namespace,
		"port-forward",
		fmt.Sprintf("svc/%s", serviceName),
		fmt.Sprintf("%d:%d", port, port),
	)
	err := kubectlCmd.Start()
	g.Expect(err).NotTo(HaveOccurred())
}
