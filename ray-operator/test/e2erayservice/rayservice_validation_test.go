package e2erayservice

import (
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceValidation(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	test.T().Run("RayService name too long with 48 characters", func(_ *testing.T) {
		rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).
			WithSpec(RayServiceSampleYamlApplyConfiguration())

		rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

		g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).
			Should(WithTransform(func(rs *rayv1.RayService) bool {
				condition := meta.FindStatusCondition(rs.Status.Conditions, string(rayv1.RayServiceReady))
				if condition == nil {
					return false
				}
				return condition.Status == "False" && condition.Reason == string(rayv1.RayServiceValidationFailed)
			}, BeTrue()))
	})
}
