package e2erayjob

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobHistoryServerSidecarInjection(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Enable RayClusterHistoryServer feature gate during test
	features.SetFeatureGateDuringTest(t, features.RayClusterHistoryServer, true)

	// Create a namespace
	namespace := test.NewTestNamespace()

	test.T().Run("RayJob with historyServerOptions should inject collector sidecars", func(t *testing.T) {
		t.Parallel()

		rayJobAC := rayv1ac.RayJob("rayjob-hs-e2e", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithEntrypoint("python -c 'print(1)'").
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

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s cluster head pod to be created", rayJob.Namespace, rayJob.Name)
		var headPod *corev1.Pod
		var underlyingCluster *rayv1.RayCluster
		g.Eventually(func() error {
			rj, err := GetRayJob(test, namespace.Name, rayJob.Name)
			if err != nil || rj.Status.RayClusterName == "" {
				return fmt.Errorf("ray cluster name not set yet")
			}
			underlyingCluster, err = GetRayCluster(test, namespace.Name, rj.Status.RayClusterName)
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
