package e2eraycronjob

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	rayclientset "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func rayCronJobACTemplate(name, namespace, schedule string) *rayv1ac.RayCronJobApplyConfiguration {
	return rayv1ac.RayCronJob(name, namespace).
		WithSpec(
			rayv1ac.RayCronJobSpec().
				WithSchedule(schedule).
				WithJobTemplate(
					rayv1ac.RayJobSpec().
						WithEntrypoint("sleep 1").
						WithRayClusterSpec(
							rayv1ac.RayClusterSpec().
								WithHeadGroupSpec(
									rayv1ac.HeadGroupSpec().
										WithTemplate(
											corev1ac.PodTemplateSpec().
												WithSpec(
													corev1ac.PodSpec().
														WithContainers(
															corev1ac.Container().
																WithName("ray-head").
																WithImage(GetRayImage()).
																WithResources(
																	corev1ac.ResourceRequirements().
																		WithRequests(corev1.ResourceList{
																			corev1.ResourceCPU:    resource.MustParse("500m"),
																			corev1.ResourceMemory: resource.MustParse("500Mi"),
																		}),
																),
														),
												),
										),
								),
						),
				),
		)
}

func TestRayCronJobSuspend(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.T().Run("RayCronJob respects suspend flag across schedule and resumes job creation.", func(_ *testing.T) {
		rayCronJobAC := rayCronJobACTemplate("suspended-raycronjob", namespace.Name, "*/1 * * * *")
		rayCronJobAC.Spec.WithSuspend(true)
		rayCronJob, err := test.Client().Ray().RayV1().RayCronJobs(namespace.Name).Apply(test.Ctx(), rayCronJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayCronJob %s/%s successfully", rayCronJob.Namespace, rayCronJob.Name)

		// Spec.suspend should be true
		g.Eventually(func() bool {
			job, err := test.Client().
				Ray().
				RayV1().
				RayCronJobs(namespace.Name).
				Get(test.Ctx(), rayCronJob.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return job.Spec.Suspend
		}, TestTimeoutShort).Should(BeTrue())

		rcj, err := test.Client().Ray().RayV1().RayCronJobs(namespace.Name).Get(test.Ctx(), rayCronJob.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		ownerUID := rcj.UID

		// No RayJob should be created
		g.Consistently(func() int {
			n, err := countRayJobsOwnedByUID(test.Ctx(), test.Client().Ray(), namespace.Name, ownerUID)
			if err != nil {
				return -1
			}
			return n
		}, 130*time.Second, 5*time.Second).Should(Equal(0))

		// Resume
		patch := []byte(`{"spec":{"suspend":false}}`)
		_, err = test.Client().Ray().RayV1().RayCronJobs(namespace.Name).Patch(test.Ctx(), rayCronJob.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		g.Expect(err).NotTo(HaveOccurred())

		// Spec.suspend should be false
		g.Eventually(func() bool {
			rcj, err := test.Client().Ray().RayV1().RayCronJobs(namespace.Name).Get(test.Ctx(), rayCronJob.Name, metav1.GetOptions{})
			return err == nil && !rcj.Spec.Suspend
		}, TestTimeoutShort).Should(BeTrue())

		// Jobs must start appearing now
		g.Eventually(func() int {
			n, err := countRayJobsOwnedByUID(test.Ctx(), test.Client().Ray(), namespace.Name, ownerUID)
			if err != nil {
				return -1
			}
			return n
		}, TestTimeoutMedium).Should(BeNumerically(">", 0))

		// Delete the RayCronJob
		err = test.Client().Ray().RayV1().RayCronJobs(namespace.Name).Delete(test.Ctx(), rayCronJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayCronJob %s/%s successfully", rayCronJob.Namespace, rayCronJob.Name)
	})
}

// countRayJobsOwnedByUID counts RayJobs in namespace whose ownerReferences contains ownerUID.
func countRayJobsOwnedByUID(ctx context.Context, rayClient rayclientset.Interface, namespace string, ownerUID types.UID) (int, error) {
	list, err := rayClient.RayV1().RayJobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range list.Items {
		owners := list.Items[i].OwnerReferences
		for _, or := range owners {
			if or.UID == ownerUID {
				n++
				break
			}
		}
	}
	return n, nil
}
