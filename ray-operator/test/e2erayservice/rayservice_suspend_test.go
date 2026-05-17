package e2erayservice

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// TestRayServiceSuspendResume covers the happy path: set Spec.Suspend=true on a
// Running RayService, observe that all owned resources are deleted and the
// status reaches Suspended, then set Spec.Suspend=false and observe that the
// service is recreated and serves traffic again.
func TestRayServiceSuspendResume(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-suspend"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(RayServiceSampleYamlApplyConfiguration())
	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be Ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	curlPod, err := CreateCurlPod(g, test, "curl-pod", "curl-container", namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())

	stdout, _ := CurlRayServicePod(test, rayService, curlPod, "curl-container", "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))

	LogWithTimestamp(test.T(), "Setting Spec.Suspend=true")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.Suspend = true
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for the Suspended condition to be True")
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceSuspended, BeTrue()))

	LogWithTimestamp(test.T(), "Asserting status fields are reset and all owned resources are gone")
	g.Eventually(func(gg Gomega) {
		rs, err := GetRayService(test, namespace.Name, rayServiceName)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(rs.Status.ActiveServiceStatus.RayClusterName).To(BeEmpty())
		gg.Expect(rs.Status.PendingServiceStatus.RayClusterName).To(BeEmpty())
		gg.Expect(rs.Status.NumServeEndpoints).To(BeEquivalentTo(0))
		gg.Expect(rs.Status.ServiceStatus).To(BeEquivalentTo(""))
		gg.Expect(IsRayServiceReady(rs)).To(BeFalse())

		rcList, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: utils.RayOriginatedFromCRNameLabelKey + "=" + rayServiceName,
		})
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(rcList.Items).To(BeEmpty())

		svcList, err := test.Client().Core().CoreV1().Services(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: utils.RayOriginatedFromCRNameLabelKey + "=" + rayServiceName,
		})
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(svcList.Items).To(BeEmpty())
	}, TestTimeoutMedium).Should(Succeed())

	LogWithTimestamp(test.T(), "Setting Spec.Suspend=false to resume")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.Suspend = false
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be Ready again", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).
		Should(WithTransform(IsRayServiceSuspended, BeFalse()))

	LogWithTimestamp(test.T(), "Sending requests to verify the resumed RayService serves traffic again")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	curlCmd := []string{
		"curl", "-sS", "-X", "POST", "-H", "Content-Type: application/json",
		fmt.Sprintf("%s-serve-svc.%s.svc.cluster.local:8000/fruit", rayService.Name, rayService.Namespace),
		"-d", `["MANGO", 2]`,
	}
	g.Eventually(func(gg Gomega) {
		stdout, _, err := ExecPodCmdWithError(test, curlPod, "curl-container", curlCmd)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(stdout.String()).To(Equal("6"))
	}, TestTimeoutShort).Should(Succeed())
}

// TestRayServiceSuspendAtomic verifies that once Suspending has been committed
// to status, deletion runs to completion even if Spec.Suspend is flipped back
// to false mid-way. The atomicity is proven by recording the original
// RayCluster name and asserting it is gone and a *different* RayCluster is
// active afterwards — this avoids depending on catching the brief window
// where the Suspended=True condition is observable.
func TestRayServiceSuspendAtomic(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-suspend-atomic"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(RayServiceSampleYamlApplyConfiguration())
	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be Ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	originalClusterName := rayService.Status.ActiveServiceStatus.RayClusterName
	g.Expect(originalClusterName).NotTo(BeEmpty())

	LogWithTimestamp(test.T(), "Setting Spec.Suspend=true to enter Suspending")
	rayService.Spec.Suspend = true
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for the Suspending condition to be True so the suspend operation is in flight")
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).
		Should(WithTransform(IsRayServiceSuspending, BeTrue()))

	LogWithTimestamp(test.T(), "Flipping Spec.Suspend back to false while suspend is in progress")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.Suspend = false
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Original RayCluster %s must be deleted (atomic completion)", originalClusterName)
	g.Eventually(func() error {
		_, err := GetRayCluster(test, namespace.Name, originalClusterName)
		return err
	}, TestTimeoutMedium).Should(WithTransform(errors.IsNotFound, BeTrue()))

	LogWithTimestamp(test.T(), "RayService should become Ready again, backed by a different RayCluster")
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService.Status.ActiveServiceStatus.RayClusterName).NotTo(BeEmpty())
	g.Expect(rayService.Status.ActiveServiceStatus.RayClusterName).NotTo(Equal(originalClusterName))
}

// TestRayServiceCreatedSuspended verifies that a RayService created with
// Spec.Suspend=true never spins up its owned resources and reaches Suspended
// directly. Flipping Spec.Suspend=false afterwards must then bring the service
// up normally.
func TestRayServiceCreatedSuspended(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-born-suspended"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).
		WithSpec(RayServiceSampleYamlApplyConfiguration().WithSuspend(true))
	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for the Suspended condition to be True")
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceSuspended, BeTrue()))

	LogWithTimestamp(test.T(), "Asserting no owned resources were ever created and status is empty")
	g.Consistently(func(gg Gomega) {
		rcList, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: utils.RayOriginatedFromCRNameLabelKey + "=" + rayServiceName,
		})
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(rcList.Items).To(BeEmpty())

		svcList, err := test.Client().Core().CoreV1().Services(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: utils.RayOriginatedFromCRNameLabelKey + "=" + rayServiceName,
		})
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(svcList.Items).To(BeEmpty())

		rs, err := GetRayService(test, namespace.Name, rayServiceName)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(rs.Status.ActiveServiceStatus.RayClusterName).To(BeEmpty())
		gg.Expect(rs.Status.PendingServiceStatus.RayClusterName).To(BeEmpty())
		gg.Expect(rs.Status.NumServeEndpoints).To(BeEquivalentTo(0))
	}, TestTimeoutShort/2).Should(Succeed())

	LogWithTimestamp(test.T(), "Setting Spec.Suspend=false; RayService should now come up normally")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.Suspend = false
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).
		Should(WithTransform(IsRayServiceSuspended, BeFalse()))
}

// TestRayServiceSuspendDuringUpgrade triggers a zero-downtime upgrade so both
// the active and pending RayClusters exist, then sets Spec.Suspend=true. Both
// clusters must be deleted and the RayService must transition to Suspended.
// After resuming, the new spec is applied and the service serves traffic.
func TestRayServiceSuspendDuringUpgrade(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-suspend-upgrade"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(RayServiceSampleYamlApplyConfiguration())
	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be Ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	LogWithTimestamp(test.T(), "Triggering a zero-downtime upgrade by bumping RayVersion")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.RayClusterSpec.RayVersion = rayService.Spec.RayClusterSpec.RayVersion + "-upgrade"
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting until a pending RayCluster is created")
	g.Eventually(func() string {
		rs, err := GetRayService(test, namespace.Name, rayServiceName)
		if err != nil {
			return ""
		}
		return rs.Status.PendingServiceStatus.RayClusterName
	}, TestTimeoutMedium).ShouldNot(BeEmpty())

	LogWithTimestamp(test.T(), "Setting Spec.Suspend=true while upgrade is in progress")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.Suspend = true
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Both active and pending RayClusters must be deleted and Suspended must reach True")
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceSuspended, BeTrue()))
	g.Eventually(func(gg Gomega) {
		rcList, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: utils.RayOriginatedFromCRNameLabelKey + "=" + rayServiceName,
		})
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(rcList.Items).To(BeEmpty())
	}, TestTimeoutMedium).Should(Succeed())

	LogWithTimestamp(test.T(), "Resuming by setting Spec.Suspend=false; RayService should become Ready with the upgraded spec")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.Suspend = false
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService.Spec.RayClusterSpec.RayVersion).To(HaveSuffix("-upgrade"))
}
