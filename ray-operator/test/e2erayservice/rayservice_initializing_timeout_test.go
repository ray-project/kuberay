package e2erayservice

import (
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// TestRayServiceBecomesReadyBeforeTimeout tests that a RayService with a timeout annotation
// successfully becomes ready within the timeout period.
func TestRayServiceBecomesReadyBeforeTimeout(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"

	// Create RayService with a generous timeout that should not be hit
	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).
		WithAnnotations(map[string]string{
			utils.RayServiceInitializingTimeoutAnnotation: "10m",
		}).
		WithSpec(RayServiceSampleYamlApplyConfiguration())

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())
	g.Expect(rayService.Annotations[utils.RayServiceInitializingTimeoutAnnotation]).To(Equal("10m"))

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Get the latest RayService
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the service is Ready and not in InitializingTimeout state
	g.Expect(IsRayServiceReady(rayService)).To(BeTrue())

	// Check that RayServiceReady condition exists and is True
	readyCondition := meta.FindStatusCondition(rayService.Status.Conditions, string(rayv1.RayServiceReady))
	g.Expect(readyCondition).NotTo(BeNil())
	g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(readyCondition.Reason).NotTo(Equal(string(rayv1.RayServiceInitializingTimeout)))
}

// TestRayServiceTimeoutAndRecovery tests that a RayService fails with InitializingTimeout
// when it cannot become ready within the configured timeout, and then recovers after spec update.
func TestRayServiceTimeoutAndRecovery(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"

	// Create RayService with a short timeout and an invalid container image
	// to ensure the head Pod never starts (ImagePullBackOff).
	// This guarantees the RayService stays in Initializing state.
	headPodTemplate := HeadPodTemplateApplyConfiguration()
	// Override the image with a non-existent one to cause ImagePullBackOff
	headPodTemplate.Spec.Containers[0].Image = ptr.To("invalid-image-does-not-exist:v1.0.0")

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).
		WithAnnotations(map[string]string{
			utils.RayServiceInitializingTimeoutAnnotation: "5s",
		}).
		WithSpec(RayServiceSampleYamlApplyConfiguration().
			WithRayClusterSpec(rayv1ac.RayClusterSpec().
				WithRayVersion(GetRayVersion()).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
					WithTemplate(headPodTemplate))))

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to timeout and fail", rayService.Namespace, rayService.Name)

	// Wait for the timeout to trigger and condition to be updated
	// Using TestTimeoutShort (1 minute) to allow for 15s timeout + reconciliation + buffer
	g.Eventually(func(g Gomega) {
		rayService, err = GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())

		// Verify RayServiceReady condition is False with InitializingTimeout reason
		readyCondition := meta.FindStatusCondition(rayService.Status.Conditions, string(rayv1.RayServiceReady))
		g.Expect(readyCondition).NotTo(BeNil())
		g.Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(readyCondition.Reason).To(Equal(string(rayv1.RayServiceInitializingTimeout)))
		g.Expect(readyCondition.Message).To(ContainSubstring("RayService failed to become ready"))
	}, TestTimeoutShort).Should(Succeed())

	// Re-fetch to get latest state after timeout
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify cluster names are cleared after timeout
	// Note: The actual RayCluster resources will be deleted after a 60-second delay,
	// but the status fields should be cleared immediately
	g.Expect(rayService.Status.ActiveServiceStatus.RayClusterName).To(BeEmpty())
	g.Expect(rayService.Status.PendingServiceStatus.RayClusterName).To(BeEmpty())

	// Test recovery: Update the RayService with valid config to trigger a new generation
	oldGeneration := rayService.Generation
	LogWithTimestamp(test.T(), "RayService timed out at generation %d, updating to valid config", oldGeneration)

	// Update with valid config - the new cluster can be created immediately
	// Note: RayCluster deletion has a 60s delay by default, but this doesn't block new cluster creation
	rayServiceUpdateAC := rayv1ac.RayService(rayServiceName, namespace.Name).
		WithAnnotations(map[string]string{
			utils.RayServiceInitializingTimeoutAnnotation: "10m",
		}).
		WithSpec(RayServiceSampleYamlApplyConfiguration())

	rayService, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceUpdateAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService.Generation).To(BeNumerically(">", oldGeneration))

	LogWithTimestamp(test.T(), "Waiting for RayService to clear timeout condition")
	g.Eventually(func(g Gomega) {
		rayService, err = GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())

		// Verify timeout condition is cleared (conditions should be empty or not InitializingTimeout)
		readyCondition := meta.FindStatusCondition(rayService.Status.Conditions, string(rayv1.RayServiceReady))
		if readyCondition != nil {
			g.Expect(readyCondition.Reason).NotTo(Equal(string(rayv1.RayServiceInitializingTimeout)))
		}
	}, TestTimeoutShort).Should(Succeed())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	// Use TestTimeoutMedium (2 minutes) to allow enough time for:
	// - New cluster creation and initialization
	// - Serve deployment
	// - Old cluster cleanup (60s delay doesn't block new cluster)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Verify final state
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(IsRayServiceReady(rayService)).To(BeTrue())
	g.Expect(rayService.Status.ActiveServiceStatus.RayClusterName).NotTo(BeEmpty())
}
