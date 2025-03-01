package sampleyaml

import (
	"path"
	"testing"

	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJob(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "ray-job.custom-head-svc.yaml",
		},
		{
			name: "ray-job.resources.yaml",
		},
		{
			name: "ray-job.sample.yaml",
		},
		{
			name: "ray-job.shutdown.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)

			yamlFilePath := path.Join(GetSampleYAMLDir(test), tt.name)
			namespace := test.NewTestNamespace()
			rayJobFromYaml := DeserializeRayJobYAML(test, yamlFilePath)
			KubectlApplyYAML(test, yamlFilePath, namespace.Name)

			rayJob, err := GetRayJob(test, namespace.Name, rayJobFromYaml.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rayJob).NotTo(BeNil())

			// Wait for RayCluster name to be populated
			g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutShort).
				Should(WithTransform(RayJobClusterName, Not(BeEmpty())))

			rayJob, err = GetRayJob(test, rayJob.Namespace, rayJob.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rayJob).NotTo(BeNil())

			test.T().Logf("Waiting for RayCluster %s/%s to be ready", namespace.Name, rayJob.Status.RayClusterName)
			g.Eventually(RayCluster(test, namespace.Name, rayJob.Status.RayClusterName), TestTimeoutMedium).
				Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
			rayCluster, err := GetRayCluster(test, namespace.Name, rayJob.Status.RayClusterName)
			g.Expect(err).NotTo(HaveOccurred())

			// Check if the RayCluster created correct number of pods
			var desiredWorkerReplicas int32
			if rayCluster.Spec.WorkerGroupSpecs != nil {
				for _, workerGroupSpec := range rayCluster.Spec.WorkerGroupSpecs {
					desiredWorkerReplicas += *workerGroupSpec.Replicas
				}
			}
			g.Eventually(WorkerPods(test, rayCluster), TestTimeoutShort).Should(HaveLen(int(desiredWorkerReplicas)))
			g.Expect(GetRayCluster(test, namespace.Name, rayCluster.Name)).To(WithTransform(RayClusterDesiredWorkerReplicas, Equal(desiredWorkerReplicas)))

			// Check if the head pod is ready
			g.Eventually(HeadPod(test, rayCluster), TestTimeoutShort).Should(WithTransform(IsPodRunningAndReady, BeTrue()))

			// Check if all worker pods are ready
			g.Eventually(WorkerPods(test, rayCluster), TestTimeoutShort).Should(WithTransform(AllPodsRunningAndReady, BeTrue()))

			g.Eventually(RayJob(test, namespace.Name, rayJobFromYaml.Name), TestTimeoutMedium).Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

			g.Eventually(RayJob(test, namespace.Name, rayJobFromYaml.Name), TestTimeoutMedium).Should(WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)))
		})
	}
}
