package e2eincrementalupgrade

import (
	"bytes"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	e2eRayService "github.com/ray-project/kuberay/ray-operator/test/e2erayservice"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func CurlRayServiceHeadService(
	t Test,
	headSvcName string,
	rayService *rayv1.RayService,
	curlPod *corev1.Pod,
	curlPodContainerName,
	rayServicePath,
	body string,
) (bytes.Buffer, bytes.Buffer) {
	cmd := []string{
		"curl",
		"-X", "POST",
		"-H", "Content-Type: application/json",
		fmt.Sprintf("%s.%s.svc.cluster.local:8000%s", headSvcName, rayService.Namespace, rayServicePath),
		"-d", body,
	}

	return ExecPodCmd(t, curlPod, curlPodContainerName, cmd)
}

func CurlRayServiceGateway(
	t Test,
	gatewayIP string,
	curlPod *corev1.Pod,
	curlPodContainerName,
	rayServicePath,
	body string,
) (bytes.Buffer, bytes.Buffer) {
	cmd := []string{
		"curl",
		"-X", "POST",
		"-H", "Content-Type: application/json",
		fmt.Sprintf("%s:80%s", gatewayIP, rayServicePath),
		"-d", body,
	}

	return ExecPodCmd(t, curlPod, curlPodContainerName, cmd)
}

func IncrementalUpgradeRayServiceApplyConfiguration(
	stepSizePercent, intervalSeconds, maxSurgePercent *int32,
) *rayv1ac.RayServiceSpecApplyConfiguration {
	spec := e2eRayService.RayServiceSampleYamlApplyConfiguration()

	spec.RayClusterSpec.EnableInTreeAutoscaling = ptr.To(true)
	spec.WithUpgradeStrategy(rayv1ac.RayServiceUpgradeStrategy().
		WithType(rayv1.IncrementalUpgrade).
		WithIncrementalUpgradeOptions(
			rayv1ac.IncrementalUpgradeOptions().
				WithGatewayClassName("istio").
				WithStepSizePercent(*stepSizePercent).
				WithIntervalSeconds(*intervalSeconds).
				WithMaxSurgePercent(*maxSurgePercent),
		),
	)

	return spec
}

// GetGatewayIP retrieves the external IP for a Gateway object
func GetGatewayIP(gateway *gwv1.Gateway) string {
	if gateway == nil {
		return ""
	}
	for _, addr := range gateway.Status.Addresses {
		if addr.Type == nil || *addr.Type == gwv1.IPAddressType {
			return addr.Value
		}
	}

	return ""
}
