package e2e

import (
	"testing"
	// "time"

	. "github.com/onsi/gomega"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "k8s.io/utils/ptr"

	// k8serrors "k8s.io/apimachinery/pkg/api/errors"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	// "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterGCSFaultTolerence(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	test.T().Log("Creating Cluster")
	annotations := map[string]string{"ray.io/ft-enabled": "true"}
	rayClusterAC := rayv1ac.RayCluster("raycluster-ok", namespace.Name).
		WithSpec(newRayClusterSpec()).WithAnnotations(annotations)
	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayClustegit r.Name)

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// redisService, err := metav1.

	headPod, err := GetHeadPod(test, rayCluster)

	test.T().Log(headPod.Name)

}
