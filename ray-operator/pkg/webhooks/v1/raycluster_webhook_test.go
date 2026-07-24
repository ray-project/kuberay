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
		disabled := false

		BeforeEach(func() {
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

		It("should fail if RedisAddress is empty", func() {
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				EnableActivePassiveHead: &enabled,
				RedisAddress:            "",
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("redisAddress must be configured"))
		})

		It("should fail if leaseDuration <= renewDeadline", func() {
			var ld int32 = 10
			var rd int32 = 10
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				EnableActivePassiveHead:            &enabled,
				RedisAddress:                       "redis:6379",
				LeaderElectionLeaseDurationSeconds: &ld,
				LeaderElectionRenewDeadlineSeconds: &rd,
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("leaderElectionLeaseDurationSeconds must be greater than leaderElectionRenewDeadlineSeconds"))
		})

		It("should fail if renewDeadline <= retryPeriod", func() {
			var rd int32 = 5
			var rp int32 = 5
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				EnableActivePassiveHead:            &enabled,
				RedisAddress:                       "redis:6379",
				LeaderElectionRenewDeadlineSeconds: &rd,
				LeaderElectionRetryPeriodSeconds:   &rp,
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("leaderElectionRenewDeadlineSeconds must be greater than leaderElectionRetryPeriodSeconds"))
		})

		It("should fail if lease parameters are zero or negative", func() {
			var zero int32 = 0
			var negative int32 = -5
			var ld int32 = 15
			var rd int32 = 10
			expectedErr := "leader election lease parameters (lease duration, renew deadline, retry period) must be greater than 0"

			// 1. leaseDuration is zero
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				EnableActivePassiveHead:            &enabled,
				RedisAddress:                       "redis:6379",
				LeaderElectionLeaseDurationSeconds: &zero,
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(expectedErr))

			// 2. renewDeadline is negative
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				EnableActivePassiveHead:            &enabled,
				RedisAddress:                       "redis:6379",
				LeaderElectionLeaseDurationSeconds: &ld,
				LeaderElectionRenewDeadlineSeconds: &negative,
			}
			err = k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(expectedErr))

			// 3. retryPeriod is zero
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				EnableActivePassiveHead:            &enabled,
				RedisAddress:                       "redis:6379",
				LeaderElectionLeaseDurationSeconds: &ld,
				LeaderElectionRenewDeadlineSeconds: &rd,
				LeaderElectionRetryPeriodSeconds:   &zero,
			}
			err = k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(expectedErr))
		})

		It("should fail if lease params are configured but EnableActivePassiveHead is false", func() {
			var ld int32 = 15
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				EnableActivePassiveHead:            &disabled,
				RedisAddress:                       "redis:6379",
				LeaderElectionLeaseDurationSeconds: &ld,
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("leader election lease parameters (lease duration, renew deadline, retry period) can only be configured when enableActivePassiveHead is true"))
		})

		It("should fail if lease params are configured but EnableActivePassiveHead is nil", func() {
			var ld int32 = 15
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				RedisAddress:                       "redis:6379",
				LeaderElectionLeaseDurationSeconds: &ld,
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("leader election lease parameters (lease duration, renew deadline, retry period) can only be configured when enableActivePassiveHead is true"))
		})

		It("should succeed if configuration is valid", func() {
			var ld int32 = 20
			var rd int32 = 15
			var rp int32 = 3
			rayCluster.Spec.GcsFaultToleranceOptions = &rayv1.GcsFaultToleranceOptions{
				EnableActivePassiveHead:            &enabled,
				RedisAddress:                       "redis:6379",
				LeaderElectionLeaseDurationSeconds: &ld,
				LeaderElectionRenewDeadlineSeconds: &rd,
				LeaderElectionRetryPeriodSeconds:   &rp,
			}
			err := k8sClient.Create(context.TODO(), &rayCluster)
			Expect(err).NotTo(HaveOccurred())

			_ = k8sClient.Delete(context.TODO(), &rayCluster)
		})
	})
})
