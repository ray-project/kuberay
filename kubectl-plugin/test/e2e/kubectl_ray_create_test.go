package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	rayClusterName = "testing-raycluster"
	rayVersion     = "2.39.0"
	rayImage       = "rayproject/ray:2.39.0"
	headCPU        = "1"
	headMemory     = "5Gi"
	workerReplica  = "2"
	workerCPU      = "3"
	workerMemory   = "6Gi"
)

var _ = Describe("Calling ray plugin `create` command to create RayCluster", Ordered, func() {
	var namespace string

	// No need to create RayCluster since this test will be creating one
	BeforeEach(func() {
		namespace = createTestNamespace()
		DeferCleanup(func() {
			deleteTestNamespace(namespace)
			namespace = ""
		})
	})

	It("succeeds in creating a RayCluster with minimal parameters", func() {
		cmd := exec.Command("kubectl", "ray", "create", "cluster", rayClusterName, "--namespace", namespace)
		output, err := cmd.CombinedOutput()

		cleanOutput := strings.TrimSpace(string(output))

		fmt.Println(cleanOutput)
		Expect(err).NotTo(HaveOccurred())

		Expect(cleanOutput).To(Equal(fmt.Sprintf("Created Ray Cluster: %s", rayClusterName)))

		// Make sure cluster is created
		cmd = exec.Command("kubectl", "ray", "get", "cluster", rayClusterName, "--namespace", namespace)
		output, err = cmd.CombinedOutput()

		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(string(output))).ToNot(BeEmpty())

		// Cleanup
		cmd = exec.Command("kubectl", "delete", "raycluster", rayClusterName, "--namespace", namespace)
		_, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
	})

	It("succeeds in creating a RayCluster with all the parameters set", func() {
		cmd := exec.Command("kubectl", "ray", "create", "cluster", rayClusterName, "--namespace", namespace, "--ray-version", rayVersion, "--head-cpu", headCPU, "--head-memory", headMemory, "--worker-replicas", workerReplica, "--worker-cpu", workerCPU, "--worker-memory", workerMemory)
		output, err := cmd.CombinedOutput()

		cleanOutput := strings.TrimSpace(string(output))

		fmt.Println(cleanOutput)
		Expect(err).NotTo(HaveOccurred())

		Expect(cleanOutput).To(Equal(fmt.Sprintf("Created Ray Cluster: %s", rayClusterName)))

		// Get cluster info but also check the values
		cmd = exec.Command("kubectl", "get", "raycluster", rayClusterName, "--namespace", namespace, "-o", "jsonpath={.spec.rayVersion}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		Expect(rayVersion).To(Equal(string(output)))

		cmd = exec.Command("kubectl", "get", "raycluster", rayClusterName, "--namespace", namespace, "-o", "jsonpath={.spec.headGroupSpec.template.spec.containers[0].image}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		Expect(rayImage).To(Equal(string(output)))

		cmd = exec.Command("kubectl", "get", "raycluster", rayClusterName, "--namespace", namespace, "-o", "jsonpath={.spec.headGroupSpec.template.spec.containers[0].resources.limits.cpu}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		Expect(headCPU).To(Equal(string(output)))

		cmd = exec.Command("kubectl", "get", "raycluster", rayClusterName, "--namespace", namespace, "-o", "jsonpath={.spec.headGroupSpec.template.spec.containers[0].resources.limits.memory}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		Expect(headMemory).To(Equal(string(output)))

		cmd = exec.Command("kubectl", "get", "raycluster", rayClusterName, "--namespace", namespace, "-o", "jsonpath={.spec.workerGroupSpecs[0].replicas}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		Expect(workerReplica).To(Equal(string(output)))

		cmd = exec.Command("kubectl", "get", "raycluster", rayClusterName, "--namespace", namespace, "-o", "jsonpath={.spec.workerGroupSpecs[0].template.spec.containers[0].resources.limits.cpu}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		Expect(workerCPU).To(Equal(string(output)))

		cmd = exec.Command("kubectl", "get", "raycluster", rayClusterName, "--namespace", namespace, "-o", "jsonpath={.spec.workerGroupSpecs[0].template.spec.containers[0].resources.limits.memory}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		Expect(workerMemory).To(Equal(string(output)))

		// Cleanup
		cmd = exec.Command("kubectl", "delete", "raycluster", rayClusterName, "--namespace", namespace)
		_, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should not succeed with creating RayCluster", func() {
		cmd := exec.Command("kubectl", "ray", "create", "cluster", "--namespace", namespace)
		output, err := cmd.CombinedOutput()

		Expect(err).To(HaveOccurred())
		Expect(string(output)).To(ContainSubstring("Error: cluster [CLUSTERNAME]"))
	})
})
