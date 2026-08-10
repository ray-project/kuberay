package v1

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

var _ = Describe("RayCluster validating webhook", func() {
	Context("when name is too long", func() {
		It("should return error", func() {
			longName := "this-name-is-tooooooooooooooooooooooooooooooooooooooooooo-long-and-should-be-invalid"
			rayCluster := rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      longName,
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{},
							},
						},
					},
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{},
				},
			}

			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())

			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("RayCluster.ray.io \"%s\" is invalid: metadata.name", longName)))
		})
	})

	Context("when name isn't a DNS1035 label", func() {
		It("should return error", func() {
			rayCluster := rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "invalid.name",
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{},
							},
						},
					},
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{},
				},
			}

			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())

			Expect(err.Error()).To(ContainSubstring("RayCluster.ray.io \"invalid.name\" is invalid: metadata.name:"))
		})
	})

	Context("when groupNames are not unique", func() {
		var name, namespace string
		var rayCluster rayv1.RayCluster

		BeforeEach(func() {
			namespace = "default"
			name = fmt.Sprintf("test-raycluster-%d", rand.IntnRange(1000, 9000))
		})

		It("should return error", func() {
			rayCluster = rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{},
							},
						},
					},
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
						{
							GroupName: "group1",
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{},
								},
							},
						},
						{
							GroupName: "group1",
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{},
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())

			Expect(err.Error()).To(ContainSubstring("worker group names must be unique"))
		})
	})

	Context("when GCS ActivePassiveHead is set", func() {
		var name, namespace string
		var rayCluster rayv1.RayCluster
		enabled := true

		BeforeEach(func() {
			features.SetFeatureGateDuringTest(GinkgoTB(), features.GCSFaultToleranceActivePassiveHead, true)
			namespace = "default"
			name = fmt.Sprintf("test-raycluster-%d", rand.IntnRange(1000, 9000))
			rayCluster = rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{},
							},
						},
					},
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{},
				},
			}
		})

		It("should fail if the GCSFaultToleranceActivePassiveHead feature gate is disabled", func() {
			features.SetFeatureGateDuringTest(GinkgoTB(), features.GCSFaultToleranceActivePassiveHead, false)
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				ActivePassiveHead: &rayv1.ActivePassiveHeadOptions{
					Enable: &enabled,
				},
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("activePassiveHead.enable requires the GCSFaultToleranceActivePassiveHead feature gate to be enabled"))
		})

		It("should fail if RedisAddress is empty", func() {
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "",
				ActivePassiveHead: &rayv1.ActivePassiveHeadOptions{
					Enable: &enabled,
				},
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("redisAddress must be configured"))
		})

		It("should fail if leaseDuration <= renewDeadline", func() {
			var ld int32 = 10
			var rd int32 = 10
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				ActivePassiveHead: &rayv1.ActivePassiveHeadOptions{
					Enable:               &enabled,
					LeaseDurationSeconds: &ld,
					RenewDeadlineSeconds: &rd,
				},
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("activePassiveHead.leaseDurationSeconds must be greater than activePassiveHead.renewDeadlineSeconds"))
		})

		It("should fail if renewDeadline <= retryPeriod", func() {
			var rd int32 = 5
			var rp int32 = 5
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				ActivePassiveHead: &rayv1.ActivePassiveHeadOptions{
					Enable:               &enabled,
					RenewDeadlineSeconds: &rd,
					RetryPeriodSeconds:   &rp,
				},
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("activePassiveHead.renewDeadlineSeconds must be greater than activePassiveHead.retryPeriodSeconds"))
		})

		It("should fail if lease parameters are less than 1", func() {
			var zero int32 = 0
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				ActivePassiveHead: &rayv1.ActivePassiveHeadOptions{
					Enable:               &enabled,
					LeaseDurationSeconds: &zero,
				},
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("leaseDurationSeconds"))
		})

		It("should succeed if configuration is valid", func() {
			var ld int32 = 20
			var rd int32 = 15
			var rp int32 = 3
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				ActivePassiveHead: &rayv1.ActivePassiveHeadOptions{
					Enable:               &enabled,
					LeaseDurationSeconds: &ld,
					RenewDeadlineSeconds: &rd,
					RetryPeriodSeconds:   &rp,
				},
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).NotTo(HaveOccurred())

			_ = k8sClient.Delete(context.TODO(), &rayCluster)
		})
	})
})
