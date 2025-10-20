package e2eincrementalupgrade

import (
	"bytes"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

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
		"--max-time", "10",
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
	return rayv1ac.RayServiceSpec().
		WithUpgradeStrategy(rayv1ac.RayServiceUpgradeStrategy().
			WithType(rayv1.IncrementalUpgrade).
			WithIncrementalUpgradeOptions(
				rayv1ac.IncrementalUpgradeOptions().
					WithGatewayClassName("istio").
					WithStepSizePercent(*stepSizePercent).
					WithIntervalSeconds(*intervalSeconds).
					WithMaxSurgePercent(*maxSurgePercent),
			)).
		WithServeConfigV2(`applications:
  - name: fruit_app
    import_path: fruit.deployment_graph
    route_prefix: /fruit
    runtime_env:
      working_dir: "https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
    deployments:
      - name: MangoStand
        num_replicas: 1
        user_config:
          price: 3
        ray_actor_options:
          num_cpus: 0.1
      - name: OrangeStand
        num_replicas: 1
        user_config:
          price: 2
        ray_actor_options:
          num_cpus: 0.1
      - name: FruitMarket
        num_replicas: 1
        ray_actor_options:
          num_cpus: 0.1
  - name: math_app
    import_path: conditional_dag.serve_dag
    route_prefix: /calc
    runtime_env:
      working_dir: "https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
    deployments:
      - name: Adder
        num_replicas: 1
        user_config:
          increment: 3
        ray_actor_options:
          num_cpus: 0.1
      - name: Multiplier
        num_replicas: 1
        user_config:
          factor: 5
        ray_actor_options:
          num_cpus: 0.1
      - name: Router
        ray_actor_options:
          num_cpus: 0.1
        num_replicas: 1`).
		WithRayClusterSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithEnableInTreeAutoscaling(true).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
				WithTemplate(corev1ac.PodTemplateSpec().
					WithSpec(corev1ac.PodSpec().
						WithRestartPolicy(corev1.RestartPolicyNever).
						WithContainers(corev1ac.Container().
							WithName("ray-head").
							WithImage(GetRayImage()).
							WithEnv(corev1ac.EnvVar().WithName(utils.RAY_ENABLE_AUTOSCALER_V2).WithValue("1")).
							WithPorts(
								corev1ac.ContainerPort().WithName(utils.GcsServerPortName).WithContainerPort(utils.DefaultGcsServerPort),
								corev1ac.ContainerPort().WithName(utils.ServingPortName).WithContainerPort(utils.DefaultServingPort),
								corev1ac.ContainerPort().WithName(utils.DashboardPortName).WithContainerPort(utils.DefaultDashboardPort),
								corev1ac.ContainerPort().WithName(utils.ClientPortName).WithContainerPort(utils.DefaultClientPort),
							).
							WithResources(corev1ac.ResourceRequirements().
								WithRequests(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("3Gi"),
								}).
								WithLimits(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("3Gi"),
								})))))).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(1).
				WithMinReplicas(1).
				WithMaxReplicas(4).
				WithRayStartParams(map[string]string{"num-cpus": "1"}).
				WithGroupName("small-group").
				WithTemplate(corev1ac.PodTemplateSpec().
					WithSpec(corev1ac.PodSpec().
						WithRestartPolicy(corev1.RestartPolicyNever).
						WithContainers(corev1ac.Container().
							WithName("ray-worker").
							WithImage(GetRayImage()).
							WithResources(corev1ac.ResourceRequirements().
								WithRequests(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("300m"),
									corev1.ResourceMemory: resource.MustParse("1G"),
								}).
								WithLimits(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1G"),
								})))))),
		)
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
