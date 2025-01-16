package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestGcsFaultToleranceOptions(t *testing.T) {
	// Each test uses a separate namespace to utilize different Redis instances
	// for better isolation.
	testCases := []struct {
		rayClusterFn  func(namespace string) *rayv1ac.RayClusterApplyConfiguration
		name          string
		redisPassword string
		createSecret  bool
	}{
		{
			name:          "No Redis Password",
			redisPassword: "",
			rayClusterFn: func(namespace string) *rayv1ac.RayClusterApplyConfiguration {
				return rayv1ac.RayCluster("raycluster-gcsft", namespace).WithSpec(
					newRayClusterSpec().WithGcsFaultToleranceOptions(
						rayv1ac.GcsFaultToleranceOptions().
							WithRedisAddress("redis:6379"),
					),
				)
			},
			createSecret: false,
		},
		{
			name:          "Redis Password",
			redisPassword: "5241590000000000",
			rayClusterFn: func(namespace string) *rayv1ac.RayClusterApplyConfiguration {
				return rayv1ac.RayCluster("raycluster-gcsft", namespace).WithSpec(
					newRayClusterSpec().WithGcsFaultToleranceOptions(
						rayv1ac.GcsFaultToleranceOptions().
							WithRedisAddress("redis:6379").
							WithRedisPassword(rayv1ac.RedisCredential().WithValue("5241590000000000")),
					),
				)
			},
			createSecret: false,
		},
		{
			name:          "Redis Password In Secret",
			redisPassword: "5241590000000000",
			rayClusterFn: func(namespace string) *rayv1ac.RayClusterApplyConfiguration {
				return rayv1ac.RayCluster("raycluster-gcsft", namespace).WithSpec(
					newRayClusterSpec().WithGcsFaultToleranceOptions(
						rayv1ac.GcsFaultToleranceOptions().
							WithRedisAddress("redis:6379").
							WithRedisPassword(rayv1ac.RedisCredential().
								WithValueFrom(v1.EnvVarSource{
									SecretKeyRef: &v1.SecretKeySelector{
										LocalObjectReference: v1.LocalObjectReference{Name: "redis-password-secret"},
										Key:                  "password",
									},
								}),
							),
					),
				)
			},
			createSecret: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)
			namespace := test.NewTestNamespace()

			deployRedis(test, namespace.Name, tc.redisPassword)

			if tc.createSecret {
				test.T().Logf("Creating Redis password secret")
				_, err := test.Client().Core().CoreV1().Secrets(namespace.Name).Apply(
					test.Ctx(),
					corev1ac.Secret("redis-password-secret", namespace.Name).
						WithStringData(map[string]string{"password": tc.redisPassword}),
					TestApplyOptions,
				)
				g.Expect(err).NotTo(HaveOccurred())
			}

			rayClusterAC := tc.rayClusterFn(namespace.Name)
			rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
			g.Expect(err).NotTo(HaveOccurred())
			test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
			g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
				Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))

			test.T().Logf("Verifying environment variables on Head Pod")
			rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Get(test.Ctx(), rayCluster.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			headPod, err := test.Client().Core().CoreV1().Pods(namespace.Name).Get(test.Ctx(), rayCluster.Status.Head.PodName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(utils.EnvVarExists(utils.RAY_REDIS_ADDRESS, headPod.Spec.Containers[utils.RayContainerIndex].Env)).Should(BeTrue())
			g.Expect(utils.EnvVarExists(utils.RAY_EXTERNAL_STORAGE_NS, headPod.Spec.Containers[utils.RayContainerIndex].Env)).Should(BeTrue())
			if tc.redisPassword == "" {
				g.Expect(utils.EnvVarExists(utils.REDIS_PASSWORD, headPod.Spec.Containers[utils.RayContainerIndex].Env)).Should(BeFalse())
			} else {
				g.Expect(utils.EnvVarExists(utils.REDIS_PASSWORD, headPod.Spec.Containers[utils.RayContainerIndex].Env)).Should(BeTrue())
			}
		})
	}
}
