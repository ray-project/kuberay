package sampleyaml

import (
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
			name: "ray-job.modin.yaml",
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

			namespace := test.NewTestNamespace()
			test.StreamKubeRayOperatorLogs()
			rayJobFromYaml := DeserializeRayJobSampleYAML(test, tt.name)
			KubectlApplyYAML(test, tt.name, namespace.Name)

			test.T().Logf("Waiting for RayJob %s/%s to be running", namespace.Name, rayJobFromYaml.Name)
			g.Eventually(RayJob(test, namespace.Name, rayJobFromYaml.Name), TestTimeoutMedium).
				Should(WithTransform(RayJobStatus, Equal(rayv1.JobStatusRunning)))

			rayJob, err := GetRayJob(test, namespace.Name, rayJobFromYaml.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rayJob).NotTo(BeNil())

			// Wait for RayCluster name to be populated
			g.Eventually(RayJobWaitForRayClusterNamePopulated(test, rayJob), TestTimeoutShort).Should(Succeed())

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
			g.Expect(rayCluster.Status.DesiredWorkerReplicas).To(Equal(desiredWorkerReplicas))

			// Check if the head pod is ready
			g.Eventually(HeadPod(test, rayCluster), TestTimeoutShort).Should(WithTransform(IsPodRunningAndReady, BeTrue()))

			// Check if all worker pods are ready
			g.Eventually(WorkerPods(test, rayCluster), TestTimeoutShort).Should(WithTransform(AllPodsRunningAndReady, BeTrue()))

			g.Eventually(GetRayJobDeploymentStatus(test, rayJob), TestTimeoutMedium).Should(Equal(rayv1.JobDeploymentStatusComplete))

			g.Eventually(GetRayJobStatus(test, rayJob), TestTimeoutMedium).Should(Equal(rayv1.JobStatusSucceeded))
		})
	}
}
