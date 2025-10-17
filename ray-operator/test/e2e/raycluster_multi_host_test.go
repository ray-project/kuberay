package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterMultiHost(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	features.SetFeatureGateDuringTest(t, features.RayMultiHostIndexing, true)

	const (
		numOfHosts      = 4
		initialReplicas = 2
		clusterName     = "raycluster-multihost"
	)
	sharedMemVolumeAC := corev1ac.Volume().
		WithName("shared-mem").
		WithEmptyDir(corev1ac.EmptyDirVolumeSource().
			WithMedium(corev1.StorageMediumMemory).
			WithSizeLimit(resource.MustParse("1Gi")),
		)

	// Define the RayCluster spec with a multi-host worker group.
	rayClusterAC := rayv1ac.RayCluster(clusterName, namespace.Name).
		WithSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithEnableInTreeAutoscaling(true).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
				WithTemplate(HeadPodTemplateApplyConfiguration().
					// All PodSpec configurations go inside WithSpec.
					WithSpec(corev1ac.PodSpec().
						WithVolumes(sharedMemVolumeAC).
						WithRestartPolicy(corev1.RestartPolicyNever).
						WithContainers(corev1ac.Container().
							WithName("ray-head").
							WithImage(GetRayImage()).
							WithEnv(corev1ac.EnvVar().WithName(utils.RAY_ENABLE_AUTOSCALER_V2).WithValue("1")).
							WithPorts(
								corev1ac.ContainerPort().WithName(utils.GcsServerPortName).WithContainerPort(utils.DefaultGcsServerPort),
								corev1ac.ContainerPort().WithName(utils.ServingPortName).WithContainerPort(utils.DefaultServingPort),
								corev1ac.ContainerPort().WithName(utils.DashboardPortName).WithContainerPort(utils.DefaultDashboardPort),
								corev1ac.ContainerPort().WithName(utils.ClientPortName).WithContainerPort(utils.DefaultClientPort),
							).
							WithResources(corev1ac.ResourceRequirements().
								WithRequests(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("3Gi"),
								}).
								WithLimits(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("3Gi"),
								})),
						),
					),
				),
			).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithGroupName("multi-host-group").
				WithReplicas(initialReplicas).
				WithMinReplicas(0).
				WithMaxReplicas(5).
				WithNumOfHosts(numOfHosts).
				WithTemplate(WorkerPodTemplateApplyConfiguration().
					// All PodSpec configurations go inside WithSpec here as well.
					WithSpec(corev1ac.PodSpec().
						WithVolumes(sharedMemVolumeAC).
						WithRestartPolicy(corev1.RestartPolicyNever).
						WithContainers(corev1ac.Container().
							WithName("ray-worker").
							WithImage(GetRayImage()).
							WithResources(corev1ac.ResourceRequirements().
								WithRequests(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("300m"),
									corev1.ResourceMemory: resource.MustParse("1G"),
								}).
								WithLimits(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1G"),
								})),
						),
					),
				),
			),
		)

	// Create the RayCluster.
	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for the cluster to become Ready and verify the initial Pod count.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutLong).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	expectedPodCount := initialReplicas * numOfHosts
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(expectedPodCount))

	// Verify that all pods are correctly labeled.
	LogWithTimestamp(test.T(), "Verifying labels on multi-host pods for %s/%s", rayCluster.Namespace, rayCluster.Name)
	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	replicaMap := make(map[string][]string)
	for _, pod := range workerPods {
		replicaName, ok := pod.Labels[utils.RayWorkerReplicaIndexKey]
		g.Expect(ok).To(BeTrue(), "Pod %s should have a replica index label", pod.Name)
		hostIndex, ok := pod.Labels[utils.RayHostIndexKey]
		g.Expect(ok).To(BeTrue(), "Pod %s should have a host index label", pod.Name)
		replicaMap[replicaName] = append(replicaMap[replicaName], hostIndex)
	}
	g.Expect(replicaMap).To(HaveLen(initialReplicas), "Should have the correct number of replica groups")
	for replicaName, hostIndices := range replicaMap {
		g.Expect(hostIndices).To(HaveLen(numOfHosts), "Replica group %s should be complete", replicaName)
	}

	// Scale down replicas from 2 to 1. Verify we scale by a multiple of NumOfHosts.
	LogWithTimestamp(test.T(), "Scaling down RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(1)
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	expectedPodCount = 1 * numOfHosts
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(expectedPodCount), "Should scale down to 1 multi-host group (4 pods)")

	// Test scale up: Increase replicas from 1 to 3.
	LogWithTimestamp(test.T(), "Scaling up RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(3)
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	expectedPodCount = 3 * numOfHosts
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(expectedPodCount), "Should scale up to 3 multi-host groups (12 pods)")

	// Manually delete a single pod and verify the controller atomically re-creates the slice.
	LogWithTimestamp(test.T(), "Testing atomic multi-host group recreation for RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	workerPods, err = GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	podToDelete := workerPods[0]
	err = test.Client().Core().CoreV1().Pods(namespace.Name).Delete(test.Ctx(), podToDelete.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	// The controller should first clean up the broken multi-host group (-4 pods), and then re-scale it up (+4 pods).
	LogWithTimestamp(test.T(), "Waiting for controller to reconcile multi-host group.")
	// Reconcilation happens too quickly to catch the state where expectedPodCount-NumOfHosts, but we can test
	// that externally deleted Pods will be re-created to satisfy the expected number.
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(expectedPodCount), "Controller restored cluster to the correct number of pods.")
}
