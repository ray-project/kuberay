package e2e

import (
	"context"
	"os/exec"
	"path"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
		getAndCheckRayJob(ctx, namespace, rayJobName, cmdOutputJobID, "SUCCEEDED", "Complete")
	})

	It("succeed in submitting RayJob with runtime environment set with working dir", func() {
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
		getAndCheckRayJob(ctx, namespace, rayJobName, cmdOutputJobID, "SUCCEEDED", "Complete")
	})

	It("succeed in submitting RayJob with headNodeSelectors and workerNodeSelectors", func() {
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

		rayJob := getAndCheckRayJob(ctx, namespace, rayJobName, cmdOutputJobID, "SUCCEEDED", "Complete")
		// Retrieve Job Head Node Selectors
		Expect(rayJob.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.NodeSelector["kubernetes.io/os"]).To(Equal("linux"))
		// Retrieve Job Worker Node Selectors
		Expect(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.NodeSelector["kubernetes.io/os"]).To(Equal("linux"))
	})
})

// `extractRayJobID` extracts the Ray Job ID from the output of `kubectl ray job submit`.
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
