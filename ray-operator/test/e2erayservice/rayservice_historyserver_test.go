package e2erayservice

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceHistoryServerSidecarInjection(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Enable RayClusterHistoryServer feature gate during test
	features.SetFeatureGateDuringTest(t, features.RayClusterHistoryServer, true)

	// Create a namespace
	namespace := test.NewTestNamespace()

	test.T().Run("RayService with historyServerOptions should inject collector sidecars", func(t *testing.T) {
		t.Parallel()

		rayServiceAC := rayv1ac.RayService("rayservice-hs-e2e", namespace.Name).
			WithSpec(rayv1ac.RayServiceSpec().
				WithRayClusterSpec(rayv1ac.RayClusterSpec().
					WithRayVersion(GetRayVersion()).
					WithHistoryServerOptions(rayv1ac.HistoryServerOptions().
						WithCollectorOptions(rayv1ac.CollectorOptions().
							WithImage("quay.io/kuberay/collector:nightly").
							WithImagePullPolicy(corev1.PullIfNotPresent).
							WithEnv(
								corev1.EnvVar{Name: "STORAGE_BACKEND", Value: "GCS"},
								corev1.EnvVar{Name: "GCS_BUCKET", Value: "test-bucket"},
								corev1.EnvVar{Name: "RAY_ROOT_DIR", Value: "test-root"},
							),
						),
					).
					WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
						WithRayStartParams(map[string]string{}).
						WithTemplate(HeadPodTemplateApplyConfiguration()),
					).
					WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
						WithReplicas(1).
						WithMinReplicas(1).
						WithMaxReplicas(1).
						WithGroupName("worker").
						WithRayStartParams(map[string]string{}).
						WithTemplate(WorkerPodTemplateApplyConfiguration()),
					),
				),
			)

		rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

		LogWithTimestamp(test.T(), "Waiting for RayService %s/%s underlying RayCluster to be created", rayService.Namespace, rayService.Name)
		var headPod *corev1.Pod
		var underlyingCluster *rayv1.RayCluster
		g.Eventually(func() error {
			rs, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Get(test.Ctx(), rayService.Name, metav1GetOptions{})
			if err != nil {
				return err
			}
			clusterName := rs.Status.ActiveServiceStatus.RayClusterName
			if clusterName == "" {
				clusterName = rs.Status.PendingServiceStatus.RayClusterName
			}
			if clusterName == "" {
				return fmt.Errorf("cluster not set yet")
			}
			underlyingCluster, err = GetRayCluster(test, namespace.Name, clusterName)
			if err != nil {
				return err
			}
			headPod, err = GetHeadPod(test, underlyingCluster)
			return err
		}, TestTimeoutMedium).Should(Succeed())

		// Verify sidecar injection in head pod
		g.Expect(headPod.Spec.Containers).To(ContainElement(
			WithTransform(func(c corev1.Container) string { return c.Name }, Equal(utils.CollectorContainerName)),
		))

		// Verify env vars on head collector container
		var headCollector corev1.Container
		for _, c := range headPod.Spec.Containers {
			if c.Name == utils.CollectorContainerName {
				headCollector = c
				break
			}
		}
		g.Expect(utils.EnvVarExists("STORAGE_BACKEND", headCollector.Env)).To(BeTrue())
		g.Expect(utils.EnvVarExists("GCS_BUCKET", headCollector.Env)).To(BeTrue())
		g.Expect(utils.EnvVarExists("RAY_ROOT_DIR", headCollector.Env)).To(BeTrue())
		g.Expect(utils.EnvVarExists("EVENTS_PORT", headCollector.Env)).To(BeTrue())
		g.Expect(utils.EnvVarExists("POD_IP", headCollector.Env)).To(BeTrue())
		g.Expect(utils.EnvVarExists("OWNER_KIND", headCollector.Env)).To(BeTrue())
		g.Expect(utils.EnvVarExists("OWNER_NAME", headCollector.Env)).To(BeTrue())

		// Verify sidecar injection in worker pod
		var workerPods []corev1.Pod
		g.Eventually(func() ([]corev1.Pod, error) {
			var err error
			workerPods, err = GetWorkerPods(test, underlyingCluster)
			if err == nil && len(workerPods) == 0 {
				return nil, fmt.Errorf("worker pods not created yet")
			}
			return workerPods, err
		}, TestTimeoutMedium).ShouldNot(BeEmpty())

		g.Expect(workerPods[0].Spec.Containers).To(ContainElement(
			WithTransform(func(c corev1.Container) string { return c.Name }, Equal(utils.CollectorContainerName)),
		))
	})
}

type metav1GetOptions = metav1.GetOptions
