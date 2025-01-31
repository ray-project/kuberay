package e2e

import (
	"testing"
	"time"

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
			name:          "Redis Password and Username",
			redisPassword: "5241590000000000",
			rayClusterFn: func(namespace string) *rayv1ac.RayClusterApplyConfiguration {
				return rayv1ac.RayCluster("raycluster-gcsft", namespace).WithSpec(
					newRayClusterSpec().WithGcsFaultToleranceOptions(
						rayv1ac.GcsFaultToleranceOptions().
							WithRedisAddress("redis:6379").
							WithRedisUsername(rayv1ac.RedisCredential().WithValue("default")).
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
		{
			name:          "Long RayCluster Name",
			redisPassword: "",
			rayClusterFn: func(namespace string) *rayv1ac.RayClusterApplyConfiguration {
				// Intentionally using a long name to test job name trimming
				return rayv1ac.RayCluster("raycluster-with-a-very-long-name-exceeding-k8s-limit", namespace).WithSpec(
					newRayClusterSpec().WithGcsFaultToleranceOptions(
						rayv1ac.GcsFaultToleranceOptions().
							WithRedisAddress("redis:6379"),
					),
				)
			},
			createSecret: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)
			namespace := test.NewTestNamespace()

			checkRedisDBSize := deployRedis(test, namespace.Name, tc.redisPassword)
			defer g.Eventually(checkRedisDBSize, time.Second*30, time.Second).Should(BeEquivalentTo("0"))

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

			err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}

func TestGcsFaultToleranceAnnotations(t *testing.T) {
	tests := []struct {
		name                          string
		storageNS                     string
		redisPasswordEnv              string
		redisPasswordInRayStartParams string
	}{
		{
			name:                          "GCS FT without redis password",
			storageNS:                     "",
			redisPasswordEnv:              "",
			redisPasswordInRayStartParams: "",
		},
		{
			name:                          "GCS FT with redis password in ray start params",
			storageNS:                     "",
			redisPasswordEnv:              "",
			redisPasswordInRayStartParams: "5241590000000000",
		},
		{
			name:                          "GCS FT with redis password in ray start params referring to env",
			storageNS:                     "",
			redisPasswordEnv:              "5241590000000000",
			redisPasswordInRayStartParams: "$REDIS_PASSWORD",
		},
		{
			name:                          "GCS FT with storage namespace",
			storageNS:                     "test-storage-ns",
			redisPasswordEnv:              "5241590000000000",
			redisPasswordInRayStartParams: "$REDIS_PASSWORD",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)
			namespace := test.NewTestNamespace()

			redisPassword := ""
			if tc.redisPasswordEnv != "" && tc.redisPasswordInRayStartParams != "" && tc.redisPasswordInRayStartParams != "$REDIS_PASSWORD" {
				t.Fatalf("redisPasswordEnv and redisPasswordInRayStartParams are both set")
			}

			switch {
			case tc.redisPasswordEnv != "":
				redisPassword = tc.redisPasswordEnv
			case tc.redisPasswordInRayStartParams != "":
				redisPassword = tc.redisPasswordInRayStartParams
			}

			checkRedisDBSize := deployRedis(test, namespace.Name, redisPassword)
			defer g.Eventually(checkRedisDBSize, time.Second*30, time.Second).Should(BeEquivalentTo("0"))

			// Prepare RayCluster ApplyConfiguration
			podTemplateAC := headPodTemplateApplyConfiguration()
			podTemplateAC.Spec.Containers[utils.RayContainerIndex].WithEnv(
				corev1ac.EnvVar().WithName("RAY_REDIS_ADDRESS").WithValue("redis:6379"),
			)
			if tc.redisPasswordEnv != "" {
				podTemplateAC.Spec.Containers[utils.RayContainerIndex].WithEnv(
					corev1ac.EnvVar().WithName("REDIS_PASSWORD").WithValue(tc.redisPasswordEnv),
				)
			}
			rayClusterAC := rayv1ac.RayCluster("raycluster-gcsft", namespace.Name).WithAnnotations(
				map[string]string{utils.RayFTEnabledAnnotationKey: "true"},
			).WithSpec(
				rayClusterSpecWith(
					rayv1ac.RayClusterSpec().
						WithRayVersion(GetRayVersion()).
						WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
							// RayStartParams are not allowed to be empty.
							WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
							WithTemplate(podTemplateAC)),
				),
			)
			if tc.storageNS != "" {
				rayClusterAC.WithAnnotations(map[string]string{utils.RayExternalStorageNSAnnotationKey: tc.storageNS})
			}
			if tc.redisPasswordInRayStartParams != "" {
				rayClusterAC.Spec.HeadGroupSpec.WithRayStartParams(map[string]string{"redis-password": tc.redisPasswordInRayStartParams})
			}

			// Apply RayCluster
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
			if redisPassword == "" {
				g.Expect(utils.EnvVarExists(utils.REDIS_PASSWORD, headPod.Spec.Containers[utils.RayContainerIndex].Env)).Should(BeFalse())
			} else {
				g.Expect(utils.EnvVarExists(utils.REDIS_PASSWORD, headPod.Spec.Containers[utils.RayContainerIndex].Env)).Should(BeTrue())
			}

			err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}
