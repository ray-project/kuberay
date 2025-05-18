package e2e

import (
	"context"
	"encoding/json"
	"os/exec"
	"path"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

// Directory when running test is kuberay/kubectl-plugin/test/e2e/
const (
	rayJobFilePath           = "./testdata/ray-job.interactive-mode.yaml"
	rayJobNoEnvFilePath      = "./testdata/ray-job.interactive-mode-no-runtime-env.yaml"
	kubectlRayJobWorkingDir  = "./testdata/rayjob-submit-working-dir/"
	entrypointSampleFileName = "entrypoint-python-sample.py"
	runtimeEnvSampleFileName = "runtime-env-sample.yaml"
	rayJobName               = "rayjob-sample"
)

var _ = Describe("Calling ray plugin `job submit` command on Ray Job", func() {
	var namespace string

	BeforeEach(func() {
		namespace = createTestNamespace()
		deployTestRayCluster(namespace)
		DeferCleanup(func() {
			deleteTestNamespace(namespace)
			namespace = ""
		})
	})

	It("succeed in submitting RayJob", func() {
		// To avoid port conflict, kill any existing port-forward process on 8265
		KillPortForwardOn8265()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(
			ctx, "kubectl", "ray", "job", "submit",
			"--namespace", namespace,
			"-f", rayJobFilePath,
			"--working-dir", kubectlRayJobWorkingDir,
			"--",
			"python", entrypointSampleFileName,
		)
		output, err := cmd.CombinedOutput()

		Expect(err).NotTo(HaveOccurred())
		// Retrieve the Job ID from the output
		cmdOutputJobID := extractRayJobID(string(output))

		// Use kubectl to check status of the rayjob
		assertRayJobSucceeded(ctx, namespace, rayJobName, cmdOutputJobID)
	})

	It("succeed in submitting RayJob with runtime environment set with working dir", func() {
		// To avoid port conflict, kill any existing port-forward process on 8265
		KillPortForwardOn8265()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		runtimeEnvFilePath := path.Join(kubectlRayJobWorkingDir, runtimeEnvSampleFileName)
		cmd := exec.CommandContext(
			ctx,
			"kubectl", "ray", "job", "submit",
			"--namespace", namespace, "-f",
			rayJobNoEnvFilePath, "--runtime-env",
			runtimeEnvFilePath,
			"--",
			"python", entrypointSampleFileName,
		)

		output, err := cmd.CombinedOutput()

		Expect(err).NotTo(HaveOccurred())
		// Retrieve the Job ID from the output
		cmdOutputJobID := extractRayJobID(string(output))

		// Use kubectl to check status of the rayjob
		assertRayJobSucceeded(ctx, namespace, rayJobName, cmdOutputJobID)
	})

	It("succeed in submitting RayJob with headNodeSelectors and workerNodeSelectors", func() {
		// To avoid port conflict, kill any existing port-forward process on 8265
		KillPortForwardOn8265()
		runtimeEnvFilePath := path.Join(kubectlRayJobWorkingDir, runtimeEnvSampleFileName)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(
			ctx,
			"kubectl", "ray", "job", "submit",
			"--namespace", namespace,
			"--name", rayJobName,
			"--runtime-env", runtimeEnvFilePath,
			"--head-cpu", "1",
			"--head-memory", "2Gi",
			"--worker-cpu", "1",
			"--worker-memory", "2Gi",
			"--head-node-selectors", "kubernetes.io/os=linux",
			"--worker-node-selectors", "kubernetes.io/os=linux",
			"--",
			"python",
			entrypointSampleFileName,
		)
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred())
		// Retrieve the Job ID from the output
		cmdOutputJobID := extractRayJobID(string(output))

		rayJob := assertRayJobSucceeded(ctx, namespace, rayJobName, cmdOutputJobID)
		// Retrieve Job Head Node Selectors
		Expect(rayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.NodeSelector["kubernetes.io/os"]).To(Equal("linux"))
		// Retrieve Job Worker Node Selectors
		Expect(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.NodeSelector["kubernetes.io/os"]).To(Equal("linux"))
	})
})

// extractRayJobID extracts the Ray Job ID from the kubectl ray job submit output.
//
// Use regex to extract the job ID from the output.
// The output is expected to be like:
//
//	Current status: RUNNING (RayJob: rayjob-sample, JobID: raysubmit_XBBxrNHztQa1rHpL)
//	Current status: SUCCEEDED (RayJob: rayjob-sample, JobID: raysubmit_XBBxrNHztQa1rHpL)
//	Job raysubmit_XBBxrNHztQa1rHpL finished with status SUCCEEDED.
//
// It returns the first matched Job ID string like "raysubmit_XBBxrNHztQa1rHpL".
func extractRayJobID(output string) string {
	// Match any string that starts with "raysubmit_" followed by alphanumeric characters.
	regexExp := regexp.MustCompile(`raysubmit_[a-zA-Z0-9]+`)

	// Find the first match in the output
	matches := regexExp.FindStringSubmatch(output)

	// Return the matched Job ID if found, otherwise return an empty string
	if len(matches) >= 1 {
		return matches[0]
	}
	return ""
}

func assertRayJobSucceeded(ctx context.Context, namespace, jobName, cmdOutputJobID string) (rayjob rayv1.RayJob) {
	cmd := exec.CommandContext(ctx, "kubectl", "get", "--namespace", namespace, "rayjob", jobName, "-o", "json")
	output, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred())

	var rayJob rayv1.RayJob
	err = json.Unmarshal(output, &rayJob)
	Expect(err).ToNot(HaveOccurred())

	// Retrieve Job ID
	Expect(cmdOutputJobID).To(Equal(rayJob.Status.JobId))
	// Retrieve Job Status
	Expect(string(rayJob.Status.JobStatus)).To(Equal("SUCCEEDED"))
	// Retrieve Job Deployment Status
	Expect(string(rayJob.Status.JobDeploymentStatus)).To(Equal("Complete"))
	return rayJob
}
