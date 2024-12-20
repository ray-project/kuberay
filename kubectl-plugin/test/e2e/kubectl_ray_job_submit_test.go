package e2e

import (
	"os/exec"
	"path"
	"regexp"

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
)

var _ = Describe("Calling ray plugin `job submit` command on Ray Job", Ordered, func() {
	It("succeed in submitting RayJob", func() {
		cmd := exec.Command("kubectl", "ray", "job", "submit", "-f", rayJobFilePath, "--working-dir", kubectlRayJobWorkingDir, "--", "python", entrypointSampleFileName)
		output, err := cmd.CombinedOutput()

		Expect(err).NotTo(HaveOccurred())
		// Retrieve the Job ID from the output
		regexExp := regexp.MustCompile(`'([^']*raysubmit[^']*)'`)
		matches := regexExp.FindStringSubmatch(string(output))

		Expect(len(matches)).To(BeNumerically(">=", 2))
		cmdOutputJobID := matches[1]

		// Use kubectl to check status of the rayjob
		// Retrieve Job ID
		cmd = exec.Command("kubectl", "get", "rayjob", "rayjob-sample", "-o", "jsonpath={.status.jobId}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		Expect(cmdOutputJobID).To(Equal(string(output)))

		// Retrieve Job Status
		cmd = exec.Command("kubectl", "get", "rayjob", "rayjob-sample", "-o", "jsonpath={.status.jobStatus}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		Expect(string(output)).To(Equal("SUCCEEDED"))

		// Retrieve Job Deployment Status
		cmd = exec.Command("kubectl", "get", "rayjob", "rayjob-sample", "-o", "jsonpath={.status.jobDeploymentStatus}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		Expect(string(output)).To(Equal("Complete"))

		// Cleanup
		cmd = exec.Command("kubectl", "delete", "rayjob", "rayjob-sample")
		_, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
	})

	It("succeed in submitting RayJob with runtime environment set with working dir", func() {
		runtimeEnvFilePath := path.Join(kubectlRayJobWorkingDir, runtimeEnvSampleFileName)
		cmd := exec.Command("kubectl", "ray", "job", "submit", "-f", rayJobNoEnvFilePath, "--runtime-env", runtimeEnvFilePath, "--", "python", entrypointSampleFileName)
		output, err := cmd.CombinedOutput()

		Expect(err).NotTo(HaveOccurred())
		// Retrieve the Job ID from the output
		regexExp := regexp.MustCompile(`'([^']*raysubmit[^']*)'`)
		matches := regexExp.FindStringSubmatch(string(output))

		Expect(len(matches)).To(BeNumerically(">=", 2))
		cmdOutputJobID := matches[1]

		// Use kubectl to check status of the rayjob
		// Retrieve Job ID
		cmd = exec.Command("kubectl", "get", "rayjob", "rayjob-sample", "-o", "jsonpath={.status.jobId}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		Expect(cmdOutputJobID).To(Equal(string(output)))

		// Retrieve Job Status
		cmd = exec.Command("kubectl", "get", "rayjob", "rayjob-sample", "-o", "jsonpath={.status.jobStatus}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		Expect(string(output)).To(Equal("SUCCEEDED"))

		// Retrieve Job Deployment Status
		cmd = exec.Command("kubectl", "get", "rayjob", "rayjob-sample", "-o", "jsonpath={.status.jobDeploymentStatus}")
		output, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		Expect(string(output)).To(Equal("Complete"))

		// Cleanup
		cmd = exec.Command("kubectl", "delete", "rayjob", "rayjob-sample")
		_, err = cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
	})
})
