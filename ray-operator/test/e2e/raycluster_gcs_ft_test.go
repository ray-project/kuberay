package e2e

import (
	. "github.com/onsi/gomega"
	"testing"
	"time"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "k8s.io/utils/ptr"

	// k8serrors "k8s.io/apimachinery/pkg/api/errors"

	// "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterGCSFaultTolerence(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	test.T().Log("Creating Cluster")
	yamlFilePath := "testdata/ray-cluster.ray-ft.yaml"
	rayClusterFromYaml := DeserializeRayClusterYAML(test, yamlFilePath)
	KubectlApplyYAML(test, yamlFilePath, namespace.Name)

	rayCluster, err := GetRayCluster(test, namespace.Name, rayClusterFromYaml.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayCluster).NotTo(BeNil())

	headPod, err := GetHeadPod(test, rayClusterFromYaml)

	
	test.T().Log(headPod.Name)

	
}
