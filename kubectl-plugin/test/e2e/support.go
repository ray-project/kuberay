package e2e

import (
	"context"
	"math/rand"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"

func randStringBytes(n int) string {
	// Reference: https://stackoverflow.com/questions/22892120/how-to-generate-a-random-string-of-a-fixed-length-in-go/22892986
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))] //nolint:gosec // Don't need cryptographically secure random number
	}
	return string(b)
}

func createTestNamespace(client Client) string {
	ctx := context.Background()
	GinkgoHelper()
	suffix := randStringBytes(5)
	ns := "test-ns-" + suffix
	nsObj, err := client.Core().CoreV1().Namespaces().Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(nsObj.Name).To(Equal(ns))
	nsWithPrefix := "namespace/" + ns
	cmd := exec.Command("kubectl", "wait", "--timeout=20s", "--for", "jsonpath={.status.phase}=Active", nsWithPrefix)
	err = cmd.Run()
	Expect(err).NotTo(HaveOccurred())
	return ns
}

func deleteTestNamespace(ns string, client Client) {
	ctx := context.Background()
	GinkgoHelper()
	err := client.Core().CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func deployTestRayCluster(ns string) {
	GinkgoHelper()
	// Print current working directory
	cmd := exec.Command("kubectl", "apply", "-f", "../../../ray-operator/config/samples/ray-cluster.sample.yaml", "-n", ns)
	err := cmd.Run()
	Expect(err).NotTo(HaveOccurred())
	cmd = exec.Command("kubectl", "wait", "--timeout=300s", "--for", "jsonpath={.status.state}=ready", "raycluster/raycluster-kuberay", "-n", ns)
	err = cmd.Run()
	Expect(err).NotTo(HaveOccurred())
}

//nolint:unparam // Currently all tests use the same param; will remove the parameter once more test cases are added
func getAndCheckRayJob(
	namespace,
	name,
	expectedJobID,
	expectedJobStatus,
	expectedJobDeploymentStatus string,
	client Client,
) (rayjob rayv1.RayJob) {
	ctx := context.Background()
	GinkgoHelper()
	rayJob, err := client.Ray().RayV1().RayJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	Expect(rayJob.Status.JobId).To(Equal(expectedJobID))
	Expect(string(rayJob.Status.JobStatus)).To(Equal(expectedJobStatus))
	Expect(string(rayJob.Status.JobDeploymentStatus)).To(Equal(expectedJobDeploymentStatus))
	return *rayJob
}
