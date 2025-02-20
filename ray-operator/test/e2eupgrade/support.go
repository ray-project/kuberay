package e2eupgrade

import (
	"os/exec"

	corev1 "k8s.io/api/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	e2e "github.com/ray-project/kuberay/ray-operator/test/e2erayservice"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func ProcessStateSuccess(cmd *exec.Cmd) bool {
	if cmd.ProcessState == nil {
		return false
	}
	return cmd.ProcessState.Success()
}

func requestRayService(t Test, rayService *rayv1.RayService, curlPod *corev1.Pod, curlContainerName string) string {
	stdout1, _ := e2e.CurlRayServicePod(t, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	stdout2, _ := e2e.CurlRayServicePod(t, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)

	return stdout1.String() + ", " + stdout2.String()
}
