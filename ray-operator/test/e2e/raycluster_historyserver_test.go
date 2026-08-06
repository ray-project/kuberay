package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterHistoryServerSidecarInjection(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Enable RayClusterHistoryServer feature gate during test
	features.SetFeatureGateDuringTest(t, features.RayClusterHistoryServer, true)

	// Create a namespace
	namespace := test.NewTestNamespace()

	test.T().Run("RayCluster with historyServerOptions should inject collector sidecars", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-hs-e2e", namespace.Name).
			WithSpec(rayv1ac.RayClusterSpec().
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
			)

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

		// Verify sidecar injection in head pod
		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
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

		// Verify sidecar injection in worker pod
		workerPods, err := GetWorkerPods(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(workerPods).NotTo(BeEmpty())
		g.Expect(workerPods[0].Spec.Containers).To(ContainElement(
			WithTransform(func(c corev1.Container) string { return c.Name }, Equal(utils.CollectorContainerName)),
		))
	})
}
