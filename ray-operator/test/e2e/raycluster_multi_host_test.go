package e2e

import (
	"maps"
	"strconv"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// verifyWorkerGroupIndices is a helper function to check that pods in a worker group
// have the correct and unique replica/host index labels.
func verifyWorkerGroupIndices(t *testing.T, rayCluster *rayv1.RayCluster, workerGroupName string, expectedHosts int, expectedReplicas int, expectedIndices []int) {
	test := With(t)
	g := NewWithT(t)

	allWorkerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	groupPods := []corev1.Pod{}
	for _, pod := range allWorkerPods {
		if pod.Labels[utils.RayNodeGroupLabelKey] == workerGroupName {
			groupPods = append(groupPods, pod)
		}
	}

	// Validate total number of pods for this group.
	expectedPodCount := expectedReplicas * expectedHosts
	g.Expect(groupPods).To(HaveLen(expectedPodCount),
		"Expected %d pods for group %s (%d replicas with %d hosts each), but found %d",
		expectedPodCount, workerGroupName, expectedReplicas, expectedHosts, len(groupPods))

	// Track the indices seen when parsing the worker Pods.
	seenReplicaIndices := make(map[int]bool)
	expectedIndicesMap := make(map[int]bool)
	for _, idx := range expectedIndices {
		expectedIndicesMap[idx] = true
	}

	if expectedHosts > 1 {
		// For multi-host, all three labels should be set.
		type ReplicaInfo struct {
			HostIndices  map[int]bool
			ReplicaIndex int
		}
		replicaGroups := make(map[string]ReplicaInfo)

		for _, pod := range groupPods {
			replicaID, ok := pod.Labels[utils.RayWorkerReplicaNameKey]
			g.Expect(ok).To(BeTrue(), "Pod %s should have a replica ID label (%s)", pod.Name, utils.RayWorkerReplicaNameKey)

			replicaIndexStr, ok := pod.Labels[utils.RayWorkerReplicaIndexKey]
			g.Expect(ok).To(BeTrue(), "Pod %s should have a replica index label (%s)", pod.Name, utils.RayWorkerReplicaIndexKey)
			replicaIndex, err := strconv.Atoi(replicaIndexStr)
			g.Expect(err).NotTo(HaveOccurred())
			seenReplicaIndices[replicaIndex] = true

			hostIndexStr, ok := pod.Labels[utils.RayHostIndexKey]
			g.Expect(ok).To(BeTrue(), "Pod %s should have a host index label (%s)", pod.Name, utils.RayHostIndexKey)
			hostIndex, err := strconv.Atoi(hostIndexStr)
			g.Expect(err).NotTo(HaveOccurred())

			// Check for duplicate host index values per replica group.
			if info, exists := replicaGroups[replicaID]; exists {
				g.Expect(replicaIndex).To(Equal(info.ReplicaIndex),
					"Pod %s in group %s has inconsistent replicaIndex. Expected %d, got %d", pod.Name, replicaID, info.ReplicaIndex, replicaIndex)

				g.Expect(info.HostIndices[hostIndex]).To(BeFalse(),
					"Pod %s in group %s has duplicate hostIndex %d", pod.Name, replicaID, hostIndex)
				info.HostIndices[hostIndex] = true
			} else {
				replicaGroups[replicaID] = ReplicaInfo{
					ReplicaIndex: replicaIndex,
					HostIndices:  map[int]bool{hostIndex: true},
				}
			}
		}

		g.Expect(replicaGroups).To(HaveLen(expectedReplicas), "Should have %d replica groups, but found %d", expectedReplicas, len(replicaGroups))
		for replicaID, info := range replicaGroups {
			g.Expect(info.HostIndices).To(HaveLen(expectedHosts), "Replica group %s should have %d hosts, but found %d", replicaID, expectedHosts, len(info.HostIndices))
		}

	} else {
		// Single-host case, only replica index is set.
		for _, pod := range groupPods {
			g.Expect(pod.Labels).NotTo(HaveKey(utils.RayWorkerReplicaNameKey), "Pod %s should not have replica ID label for single-host group", pod.Name)
			g.Expect(pod.Labels).NotTo(HaveKey(utils.RayHostIndexKey), "Pod %s should not have host index label for single-host group", pod.Name)

			// Check for unique replica index label
			indexStr, ok := pod.Labels[utils.RayWorkerReplicaIndexKey]
			g.Expect(ok).To(BeTrue(), "Pod %s should have a replica index label (%s)", pod.Name, utils.RayWorkerReplicaIndexKey)

			index, err := strconv.Atoi(indexStr)
			g.Expect(err).NotTo(HaveOccurred(), "Failed to parse replica index '%s' for pod %s", indexStr, pod.Name)

			g.Expect(seenReplicaIndices[index]).To(BeFalse(), "Found duplicate replica index %d for pod %s", index, pod.Name)
			seenReplicaIndices[index] = true
		}
	}

	if expectedIndices != nil {
		expectedIndicesMap := make(map[int]bool)
		for _, idx := range expectedIndices {
			expectedIndicesMap[idx] = true
		}
		g.Expect(seenReplicaIndices).To(Equal(expectedIndicesMap),
			"Expected replica indices %v for group %s, but found %v", expectedIndicesMap, workerGroupName, seenReplicaIndices)
	}
}

// verifyHeadlessServiceExists checks that the RayCluster headless Service exists and is headless.
func verifyHeadlessServiceExists(t *testing.T, rayCluster *rayv1.RayCluster) {
	test := With(t)
	g := NewWithT(t)

	expectedName := rayCluster.Name + utils.DashSymbol + utils.HeadlessServiceSuffix
	svc, err := test.Client().Core().CoreV1().Services(rayCluster.Namespace).Get(test.Ctx(), expectedName, metav1.GetOptions{})

	g.Expect(err).NotTo(HaveOccurred(), "Headless service %s/%s should exist", rayCluster.Namespace, expectedName)
	g.Expect(svc.Spec.ClusterIP).To(Equal(corev1.ClusterIPNone), "Service %s should be headless", expectedName)
	g.Expect(svc.Labels[utils.RayClusterHeadlessServiceLabelKey]).To(Equal(rayCluster.Name))
}

// verifyWorkerPodFQDNs checks that each worker Pod has hostname/subdomain matching its replica
// (and host) index so the stable Pod FQDN can resolve via the headless Service.
func verifyWorkerPodFQDNs(t *testing.T, rayCluster *rayv1.RayCluster, workerGroupName string, numOfHosts int) {
	test := With(t)
	g := NewWithT(t)

	expectedSubdomain := rayCluster.Name + utils.DashSymbol + utils.HeadlessServiceSuffix
	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())

	for _, pod := range workerPods {
		if pod.Labels[utils.RayNodeGroupLabelKey] != workerGroupName {
			continue
		}

		g.Expect(pod.Spec.Subdomain).To(Equal(expectedSubdomain),
			"Pod %s subdomain should match headless service name", pod.Name)

		replicaIndex := pod.Labels[utils.RayWorkerReplicaIndexKey]
		g.Expect(replicaIndex).NotTo(BeEmpty(), "Pod %s missing replica index label", pod.Name)

		var expectedHostname string
		if numOfHosts > 1 {
			hostIndex := pod.Labels[utils.RayHostIndexKey]
			g.Expect(hostIndex).NotTo(BeEmpty(), "Pod %s missing host index label", pod.Name)
			expectedHostname = workerGroupName + utils.DashSymbol + replicaIndex + utils.DashSymbol + hostIndex
		} else {
			expectedHostname = workerGroupName + utils.DashSymbol + replicaIndex
		}
		g.Expect(pod.Spec.Hostname).To(Equal(expectedHostname),
			"Pod %s hostname should be stable from group/replica indices", pod.Name)
	}
}

func TestRayClusterSingleHostMultiSlice(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	features.SetFeatureGateDuringTest(t, features.RayMultiHostIndexing, true)

	namespace := test.NewTestNamespace()
	const (
		initialReplicas = 3
		clusterName     = "raycluster-singlehost"
		workerGroupName = "single-host-group"
	)

	// Define the RayCluster spec with a single-host worker group (NumOfHosts = 1).
	rayClusterAC := rayv1ac.RayCluster(clusterName, namespace.Name).
		WithSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"num-cpus": "0"}).
				WithTemplate(HeadPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(initialReplicas).
				WithMinReplicas(0).
				WithMaxReplicas(5).
				WithNumOfHosts(1).
				WithGroupName("single-host-group").
				WithRayStartParams(map[string]string{"num-cpus": "1"}).
				WithTemplate(WorkerPodTemplateApplyConfiguration())))

	// Create the RayCluster.
	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(t, "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for the cluster to become Ready and verify the initial Pod count.
	LogWithTimestamp(t, "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutLong).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(initialReplicas))

	// Verify that all pods are correctly labeled with indices 0 to replicas-1.
	LogWithTimestamp(t, "Verifying initial labels on single-host pods for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyWorkerGroupIndices(t, rayCluster, workerGroupName, 1, initialReplicas, []int{0, 1, 2})

	// Verify headless service and stable worker Pod FQDNs (hostname + subdomain).
	LogWithTimestamp(t, "Verifying headless service and worker FQDNs for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyHeadlessServiceExists(t, rayCluster)
	verifyWorkerPodFQDNs(t, rayCluster, workerGroupName, 1)

	// Manually delete the pod with replica index 1.
	LogWithTimestamp(t, "Deleting pod with replica index 1 for %s/%s", rayCluster.Namespace, rayCluster.Name)
	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())

	var podToDelete *corev1.Pod
	for _, pod := range workerPods {
		if pod.Labels[utils.RayWorkerReplicaIndexKey] == "1" {
			podToDelete = &pod
			break
		}
	}
	g.Expect(podToDelete).NotTo(BeNil(), "Could not find pod with replica index 1 to delete")
	deletedPodUID := podToDelete.UID
	deletedHostname := podToDelete.Spec.Hostname
	deletedSubdomain := podToDelete.Spec.Subdomain

	err = test.Client().Core().CoreV1().Pods(namespace.Name).Delete(test.Ctx(), podToDelete.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(t, "Deleted pod %s", podToDelete.Name)

	// Wait for the controller to reconcile. The pod count should return to 3.
	LogWithTimestamp(t, "Waiting for controller to reconcile and fill the gap")
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(initialReplicas), "Controller should restore pod count to %d", initialReplicas)

	// Verify that the controller replaced the missing index by creating a new pod with index 1.
	LogWithTimestamp(t, "Verifying labels after pod deletion and reconciliation")
	verifyWorkerGroupIndices(t, rayCluster, workerGroupName, 1, initialReplicas, []int{0, 1, 2})

	// Verify the replacement keeps the same stable FQDN (hostname/subdomain) with a new Pod UID.
	LogWithTimestamp(t, "Verifying stable FQDN after worker recreate for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyHeadlessServiceExists(t, rayCluster)
	verifyWorkerPodFQDNs(t, rayCluster, workerGroupName, 1)
	g.Eventually(func(gg Gomega) {
		pods, err := GetWorkerPods(test, rayCluster)
		gg.Expect(err).NotTo(HaveOccurred())

		var replacement *corev1.Pod
		for i := range pods {
			if pods[i].Labels[utils.RayWorkerReplicaIndexKey] == "1" && pods[i].DeletionTimestamp == nil {
				replacement = &pods[i]
				break
			}
		}

		gg.Expect(replacement).NotTo(BeNil(), "Could not find replacement pod with replica index 1")
		gg.Expect(replacement.UID).NotTo(Equal(deletedPodUID), "Replacement pod should be a new object")
		gg.Expect(replacement.Spec.Hostname).To(Equal(deletedHostname), "Hostname should survive recreate")
		gg.Expect(replacement.Spec.Subdomain).To(Equal(deletedSubdomain), "Subdomain should survive recreate")
	}, TestTimeoutShort).Should(Succeed())

	// Scale up replicas from 3 to 4.
	const scaleUpReplicas = 4
	LogWithTimestamp(t, "Scaling up RayCluster %s/%s from %d to %d replicas", rayCluster.Namespace, rayCluster.Name, initialReplicas, scaleUpReplicas)
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(scaleUpReplicas)
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(scaleUpReplicas), "Should scale up to %d pods", scaleUpReplicas)

	// Verify the new pod got the next available index, 3.
	LogWithTimestamp(t, "Verifying labels after scale-up")
	verifyWorkerGroupIndices(t, rayCluster, workerGroupName, 1, scaleUpReplicas, []int{0, 1, 2, 3})

	// Verify FQDNs remain correct after scale-up (including new index 3).
	LogWithTimestamp(t, "Verifying worker FQDNs after scale-up for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyWorkerPodFQDNs(t, rayCluster, workerGroupName, 1)
}

func TestRayClusterMultiHostMultiSlice(t *testing.T) {
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

	// Define the RayCluster spec with a multi-host worker group.
	rayClusterAC := rayv1ac.RayCluster(clusterName, namespace.Name).
		WithSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
				WithTemplate(HeadPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(initialReplicas).
				WithMinReplicas(0).
				WithMaxReplicas(5).
				WithNumOfHosts(numOfHosts).
				WithGroupName("multi-host-group").
				WithRayStartParams(map[string]string{"num-cpus": "1"}).
				WithTemplate(WorkerPodTemplateApplyConfiguration())))

	// Create the RayCluster.
	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(t, "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for the cluster to become Ready and verify the initial Pod count.
	LogWithTimestamp(t, "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutLong).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	expectedPodCount := initialReplicas * numOfHosts
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(expectedPodCount))

	// Verify that all pods are correctly labeled during replica group scale up.
	LogWithTimestamp(t, "Verifying labels on multi-host pods for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyWorkerGroupIndices(t, rayCluster, "multi-host-group", numOfHosts, initialReplicas, []int{0, 1})

	// Scale down replicas from 2 to 1. Verify we scale by a multiple of NumOfHosts.
	const scaleDownReplicas = 1
	LogWithTimestamp(t, "Scaling down RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(1)
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	expectedPodCount = scaleDownReplicas * numOfHosts
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(expectedPodCount), "Should scale down to 1 multi-host group (4 pods)")

	// Verify labels again after replica group scale down.
	LogWithTimestamp(t, "Verifying labels after scale-down for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyWorkerGroupIndices(t, rayCluster, "multi-host-group", numOfHosts, scaleDownReplicas, nil)

	// Verify FQDNs remain correct after scale-down.
	LogWithTimestamp(t, "Verifying worker FQDNs after scale-down for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyHeadlessServiceExists(t, rayCluster)
	verifyWorkerPodFQDNs(t, rayCluster, "multi-host-group", numOfHosts)

	// Test scale up: Increase replicas from 1 to 3.
	const scaleUpReplicas = 3
	LogWithTimestamp(t, "Scaling up RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(scaleUpReplicas)
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	expectedPodCount = scaleUpReplicas * numOfHosts
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(expectedPodCount), "Should scale up to 3 multi-host groups (12 pods)")

	// Verify labels are set with expected values after scale up again.
	LogWithTimestamp(t, "Verifying labels after scale-up for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyWorkerGroupIndices(t, rayCluster, "multi-host-group", numOfHosts, scaleUpReplicas, []int{0, 1, 2})

	// Verify FQDNs remain correct after scale-up.
	LogWithTimestamp(t, "Verifying worker FQDNs after scale-up for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyHeadlessServiceExists(t, rayCluster)
	verifyWorkerPodFQDNs(t, rayCluster, "multi-host-group", numOfHosts)

	// Manually delete a single pod and verify the controller atomically re-creates the slice.
	LogWithTimestamp(t, "Testing atomic multi-host group recreation for RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())

	podToDelete := workerPods[0]
	deletedReplicaName := podToDelete.Labels[utils.RayWorkerReplicaNameKey]
	deletedReplicaIndex := podToDelete.Labels[utils.RayWorkerReplicaIndexKey]
	deletedSubdomain := podToDelete.Spec.Subdomain

	g.Expect(deletedReplicaName).NotTo(BeEmpty(), "Deleted pod should have a replica name label")
	g.Expect(deletedReplicaIndex).NotTo(BeEmpty(), "Deleted pod should have a replica index label")

	// Capture the full replica slice before delete so we can assert every host is replaced.
	oldSliceUIDs := map[types.UID]struct{}{}
	oldSliceHostnames := map[string]struct{}{}
	for _, pod := range workerPods {
		if pod.Labels[utils.RayWorkerReplicaNameKey] != deletedReplicaName {
			continue
		}
		oldSliceUIDs[pod.UID] = struct{}{}
		oldSliceHostnames[pod.Spec.Hostname] = struct{}{}
	}

	g.Expect(oldSliceUIDs).To(HaveLen(numOfHosts), "Expected %d pods in the replica slice before delete", numOfHosts)
	g.Expect(oldSliceHostnames).To(HaveLen(numOfHosts), "Expected %d unique hostnames in the replica slice before delete", numOfHosts)

	err = test.Client().Core().CoreV1().Pods(namespace.Name).Delete(test.Ctx(), podToDelete.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	// The controller should first clean up the broken multi-host group (-NumOfHosts pods), and then re-scale it up (+NumOfHosts pods).
	LogWithTimestamp(t, "Waiting for controller to reconcile multi-host group.")
	// Reconciliation happens too quickly to catch the state where expectedPodCount-NumOfHosts, but we can test
	// that externally deleted Pods will be re-created to satisfy the expected number.
	g.Eventually(func() ([]corev1.Pod, error) {
		return GetWorkerPods(test, rayCluster)
	}, TestTimeoutShort).Should(HaveLen(expectedPodCount), "Controller restored cluster to the correct number of pods.")

	// Verify labels are still set correctly after atomic re-creation due to unhealthy Pod.
	LogWithTimestamp(t, "Verifying labels after atomic recreation for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyWorkerGroupIndices(t, rayCluster, "multi-host-group", numOfHosts, scaleUpReplicas, []int{0, 1, 2})

	// Verify the entire replica slice was replaced: no old UIDs remain, all hostnames return on new Pods.
	LogWithTimestamp(t, "Verifying full replica-slice recreate and FQDNs for %s/%s", rayCluster.Namespace, rayCluster.Name)
	verifyHeadlessServiceExists(t, rayCluster)
	verifyWorkerPodFQDNs(t, rayCluster, "multi-host-group", numOfHosts)
	g.Eventually(func(gg Gomega) {
		pods, err := GetWorkerPods(test, rayCluster)
		gg.Expect(err).NotTo(HaveOccurred())

		newSlice := []corev1.Pod{}
		for _, pod := range pods {
			if pod.DeletionTimestamp != nil {
				continue
			}
			// Match by replica index: replica name is regenerated on recreate.
			if pod.Labels[utils.RayWorkerReplicaIndexKey] == deletedReplicaIndex &&
				pod.Labels[utils.RayNodeGroupLabelKey] == "multi-host-group" {
				newSlice = append(newSlice, pod)
			}
		}
		gg.Expect(newSlice).To(HaveLen(numOfHosts), "Replica index %s should again have %d hosts", deletedReplicaIndex, numOfHosts)

		remainingHostnames := maps.Clone(oldSliceHostnames)
		for _, pod := range newSlice {
			_, wasOldUID := oldSliceUIDs[pod.UID]

			gg.Expect(wasOldUID).To(BeFalse(), "Pod %s still has an old slice UID %s", pod.Name, pod.UID)
			gg.Expect(pod.Spec.Subdomain).To(Equal(deletedSubdomain), "Subdomain should survive recreate")

			_, ok := remainingHostnames[pod.Spec.Hostname]
			gg.Expect(ok).To(BeTrue(), "Pod %s has unexpected or duplicate hostname %s", pod.Name, pod.Spec.Hostname)
			delete(remainingHostnames, pod.Spec.Hostname)
		}
		gg.Expect(remainingHostnames).To(BeEmpty(), "Missing hostnames from the deleted slice: %v", remainingHostnames)
	}, TestTimeoutShort).Should(Succeed())
}
