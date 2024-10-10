package sampleyaml

import (
	"testing"

	"github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayCluster(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	tests := []struct {
		name string
	}{
		{
			name: "ray-cluster.complete.yaml",
		},
		{
			name: "ray-cluster.custom-head-service.yaml",
		},
		{
			name: "ray-cluster.embed-grafana.yaml",
		},
		{
			name: "ray-cluster.sample.yaml",
		},
	}

	for _, tt := range tests {
		test.T().Run(tt.name, func(_ *testing.T) {
			rayClusterFromYaml := DeserializeRayClusterSampleYAML(test, tt.name)
			rayCluster := rayClusterFromYaml.DeepCopy()
			rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Create(test.Ctx(), rayCluster, TestCreateOptions)
			test.Expect(err).NotTo(gomega.HaveOccurred())
			test.Expect(rayCluster).NotTo(gomega.BeNil())
			test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			test.T().Logf("Waiting for RayCluster %s/%s to be ready", rayCluster.Namespace, rayCluster.Name)
			test.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			rayCluster = GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)

			// Check if the RayCluster created correct number of pods
			var desiredWorkerReplicas int32
			if rayCluster.Spec.WorkerGroupSpecs != nil {
				desiredWorkerReplicas = *rayClusterFromYaml.Spec.WorkerGroupSpecs[0].Replicas
			}
			test.Expect(rayCluster.Status.DesiredWorkerReplicas).To(gomega.Equal(desiredWorkerReplicas))
		})
	}
}
