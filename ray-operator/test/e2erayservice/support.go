package e2erayservice

import (
	"bytes"
	"embed"
	"fmt"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

//go:embed *.py
var _files embed.FS

func ReadFile(t Test, fileName string) []byte {
	t.T().Helper()
	file, err := _files.ReadFile(fileName)
	require.NoError(t.T(), err)
	return file
}

type option[T any] func(t *T) *T

func apply[T any](t *T, options ...option[T]) *T {
	for _, opt := range options {
		t = opt(t)
	}
	return t
}

func options[T any](options ...option[T]) option[T] {
	return func(t *T) *T {
		for _, opt := range options {
			t = opt(t)
		}
		return t
	}
}

func newConfigMap(namespace string, options ...option[corev1ac.ConfigMapApplyConfiguration]) *corev1ac.ConfigMapApplyConfiguration {
	cmAC := corev1ac.ConfigMap("locust-runner-script", namespace).
		WithBinaryData(map[string][]byte{}).
		WithImmutable(true)

	return configMapWith(cmAC, options...)
}

func configMapWith(configMapAC *corev1ac.ConfigMapApplyConfiguration, options ...option[corev1ac.ConfigMapApplyConfiguration]) *corev1ac.ConfigMapApplyConfiguration {
	return apply(configMapAC, options...)
}

func file(t Test, fileName string) option[corev1ac.ConfigMapApplyConfiguration] {
	return func(cmAC *corev1ac.ConfigMapApplyConfiguration) *corev1ac.ConfigMapApplyConfiguration {
		cmAC.WithBinaryData(map[string][]byte{fileName: ReadFile(t, fileName)})
		return cmAC
	}
}

func files(t Test, fileNames ...string) option[corev1ac.ConfigMapApplyConfiguration] {
	var files []option[corev1ac.ConfigMapApplyConfiguration]
	for _, fileName := range fileNames {
		files = append(files, file(t, fileName))
	}
	return options(files...)
}

func CurlRayServicePod(
	t Test,
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
		fmt.Sprintf("%s-serve-svc.%s.svc.cluster.local:8000%s", rayService.Name, rayService.Namespace, rayServicePath),
		"-d", body,
	}

	return ExecPodCmd(t, curlPod, curlPodContainerName, cmd)
}

func curlHeadPodWithRayServicePath(t Test,
	rayCluster *rayv1.RayCluster,
	curlPod *corev1.Pod,
	curlPodContainerName,
	rayServicePath string,
	body string,
) (bytes.Buffer, bytes.Buffer) {
	cmd := []string{
		"curl",
		"-X", "GET",
		"-H", "Content-Type: application/json",
		fmt.Sprintf("%s-head-svc.%s.svc.cluster.local:8000%s", rayCluster.Name, rayCluster.Namespace, rayServicePath),
		"-d", body,
	}
	return ExecPodCmd(t, curlPod, curlPodContainerName, cmd)
}

func RayServiceSampleYamlApplyConfiguration() *rayv1ac.RayServiceSpecApplyConfiguration {
	return rayv1ac.RayServiceSpec().WithServeConfigV2(`applications:
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
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
				WithTemplate(corev1ac.PodTemplateSpec().
					WithSpec(corev1ac.PodSpec().
						WithContainers(corev1ac.Container().
							WithName("ray-head").
							WithImage(GetRayImage()).
							WithPorts(
								corev1ac.ContainerPort().WithName(utils.GcsServerPortName).WithContainerPort(utils.DefaultGcsServerPort),
								corev1ac.ContainerPort().WithName(utils.ServingPortName).WithContainerPort(utils.DefaultServingPort),
								corev1ac.ContainerPort().WithName(utils.DashboardPortName).WithContainerPort(utils.DefaultDashboardPort),
								corev1ac.ContainerPort().WithName(utils.ClientPortName).WithContainerPort(utils.DefaultClientPort),
							).
							WithResources(corev1ac.ResourceRequirements().
								WithRequests(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								}).
								WithLimits(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("3Gi"),
								})))))),
		)
}

func waitingForRayClusterSwitch(g *WithT, test Test, rayService *rayv1.RayService, oldRayClusterName string) {
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be true", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

	// Assert that the active RayCluster is eventually different
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to switch to a new cluster", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(func(rayService *rayv1.RayService) string {
		return rayService.Status.ActiveServiceStatus.RayClusterName
	}, Not(Equal(oldRayClusterName))))

	LogWithTimestamp(test.T(), "Verifying RayService %s/%s UpgradeInProgress condition to be false", rayService.Namespace, rayService.Name)
	rayService, err := GetRayService(test, rayService.Namespace, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(IsRayServiceUpgrading(rayService)).To(BeFalse())
}
