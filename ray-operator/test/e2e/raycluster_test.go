package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterManagedBy(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	test.T().Run("Successful creation of cluster, managed by Kuberay Operator", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-ok", namespace.Name).
			WithSpec(newRayClusterSpec().
				WithManagedBy(utils.KubeRayController))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	})

	test.T().Run("Creation of cluster skipped, managed by Kueue", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-skip", namespace.Name).
			WithSpec(newRayClusterSpec().
				WithManagedBy("kueue.x-k8s.io/multikueue"))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		test.T().Logf("RayCluster %s/%s will not become ready - not reconciled", rayCluster.Namespace, rayCluster.Name)
		g.Consistently(func(gg Gomega) {
			rc, err := RayCluster(test, rayCluster.Namespace, rayCluster.Name)()
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(rc.Status.Conditions).To(BeEmpty())
		}, time.Second*3, time.Millisecond*500).Should(Succeed())

		// Should not to be able to change managedBy field as it's immutable
		rayClusterAC.Spec.WithManagedBy(utils.KubeRayController)
		rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).To(HaveOccurred())
		g.Eventually(RayCluster(test, *rayClusterAC.Namespace, *rayClusterAC.Name)).
			Should(WithTransform(RayClusterManagedBy, Equal(ptr.To("kueue.x-k8s.io/multikueue"))))
	})

	test.T().Run("Failed creation of cluster, managed by external non supported controller", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-fail", namespace.Name).
			WithSpec(newRayClusterSpec().
				WithManagedBy("controller.com/not-supported"))

		_, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
	})
}

func TestRayClusterSuspend(t *testing.T) {
	test := With(t)
	g := NewWithT(t)
	// Create a namespace
	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("raycluster-suspend", namespace.Name).WithSpec(newRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))

	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(true))
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Suspend RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	test.T().Logf("Waiting for RayCluster %s/%s to be suspended", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterSuspended), MatchCondition(metav1.ConditionTrue, string(rayv1.RayClusterSuspended))))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionFalse, rayv1.HeadPodNotFound)))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionFalse, rayv1.RayClusterPodsProvisioning)))

	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(false))
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Resume RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	test.T().Logf("Waiting for RayCluster %s/%s to be resumed", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterSuspended), MatchCondition(metav1.ConditionFalse, string(rayv1.RayClusterSuspended))))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))
}

func TestRayClusterGCSFT(t *testing.T) {
	test := With(t)
	g := NewWithT(t)
	// Create a namespace
	namespace := test.NewTestNamespace()

	_, err := test.Client().Core().AppsV1().Deployments(namespace.Name).Apply(
		test.Ctx(),
		appsv1ac.Deployment("redis", namespace.Name).
			WithSpec(appsv1ac.DeploymentSpec().
				WithReplicas(1).
				WithSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{"app": "redis"})).
				WithTemplate(corev1ac.PodTemplateSpec().
					WithLabels(map[string]string{"app": "redis"}).
					WithSpec(corev1ac.PodSpec().
						WithContainers(corev1ac.Container().
							WithName("redis").
							WithImage("redis:7.4").
							WithPorts(corev1ac.ContainerPort().WithContainerPort(6379)),
						),
					),
				),
			),
		TestApplyOptions,
	)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = test.Client().Core().CoreV1().Services(namespace.Name).Apply(
		test.Ctx(),
		corev1ac.Service("redis", namespace.Name).
			WithSpec(corev1ac.ServiceSpec().
				WithSelector(map[string]string{"app": "redis"}).
				WithPorts(corev1ac.ServicePort().
					WithPort(6379),
				),
			),
		TestApplyOptions,
	)
	g.Expect(err).NotTo(HaveOccurred())

	rayClusterAC := rayv1ac.RayCluster("raycluster-gcsft", namespace.Name).WithSpec(
		newRayClusterSpec().WithGcsFaultToleranceOptions(
			rayv1ac.GcsFaultToleranceOptions().
				WithRedisAddress("redis:6379"),
		),
	)

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Make sure the RAY_REDIS_ADDRESS env is set on the Head Pod.
	g.Eventually(func(g Gomega) bool {
		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		if rayCluster.Status.Head.PodName != "" {
			headPod, err := test.Client().Core().CoreV1().Pods(namespace.Name).Get(test.Ctx(), rayCluster.Status.Head.PodName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			return utils.EnvVarExists(utils.RAY_REDIS_ADDRESS, headPod.Spec.Containers[utils.RayContainerIndex].Env)
		}
		return false
	}, TestTimeoutMedium).Should(BeTrue())

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))
}
