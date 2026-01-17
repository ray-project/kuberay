package support

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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

// getContainerStatusByName retrieves the container status by container name.
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
