package e2e

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Calling ray plugin `session` command", func() {
	var namespace string

	BeforeEach(func() {
		namespace = createTestNamespace()
		deployTestRayCluster(namespace)
		DeferCleanup(func() {
			deleteTestNamespace(namespace)
			namespace = ""
		})
	})

	It("succeed in forwarding RayCluster and should be able to cancel", func() {
		cmd := exec.Command("kubectl", "ray", "session", "--namespace", namespace, "raycluster-kuberay")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := cmd.Start()
		Expect(err).NotTo(HaveOccurred())

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.Command("curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 3*time.Second, 500*time.Millisecond).ShouldNot(HaveOccurred())

		// Send a signal to cancel the command
		err = cmd.Process.Signal(os.Interrupt)
		Expect(err).NotTo(HaveOccurred())

		select {
		case <-ctx.Done():
			// Timeout, kill the process
			Expect(ctx.Err()).To(Equal(context.DeadlineExceeded))
			err = cmd.Process.Kill()
			Expect(err).NotTo(HaveOccurred())
			Fail("kubectl ray session command did not finish in time")
		case err = <-done:
			// It should not have error, or ExitError due to interrupt
			if err != nil {
				exitErr := &exec.ExitError{}
				Expect(errors.As(err, &exitErr)).To(BeTrue())
				Expect(exitErr.String()).To(Equal("signal: interrupt"))
			}
		}
	})

	It("should reconnect after pod connection is lost", func() {
		Skip("Skip this because it is flaky now")
		sessionCmd := exec.Command("kubectl", "ray", "session", "--namespace", namespace, "raycluster-kuberay")

		err := sessionCmd.Start()
		Expect(err).NotTo(HaveOccurred())

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.Command("curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 3*time.Second, 500*time.Millisecond).ShouldNot(HaveOccurred())

		// Get the current head pod name
		cmd := exec.Command("kubectl", "get", "--namespace", namespace, "raycluster/raycluster-kuberay", "-o", "jsonpath={.status.head.podName}")
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred())
		oldPodName := string(output)
		var newPodName string

		// Delete the pod
		cmd = exec.Command("kubectl", "delete", "--namespace", namespace, "pod", oldPodName)
		err = cmd.Run()
		Expect(err).NotTo(HaveOccurred())

		// Wait for the new pod to be created
		Eventually(func() error {
			cmd := exec.Command("kubectl", "get", "--namespace", namespace, "raycluster/raycluster-kuberay", "-o", "jsonpath={.status.head.podName}")
			output, err := cmd.CombinedOutput()
			newPodName = string(output)
			if err != nil {
				return err
			}
			if string(output) == oldPodName {
				return err
			}
			return nil
		}, 60*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

		// Wait for the new pod to be ready
		cmd = exec.Command("kubectl", "wait", "--namespace", namespace, "pod", newPodName, "--for=condition=Ready", "--timeout=60s")
		err = cmd.Run()
		Expect(err).NotTo(HaveOccurred())

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.Command("curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 60*time.Second, 1*time.Millisecond).ShouldNot(HaveOccurred())

		err = sessionCmd.Process.Kill()
		Expect(err).NotTo(HaveOccurred())
		_ = sessionCmd.Wait()
	})

	It("should not succeed", func() {
		cmd := exec.Command("kubectl", "ray", "session", "--namespace", namespace, "fakeclustername")
		output, err := cmd.CombinedOutput()

		Expect(err).To(HaveOccurred())
		Expect(output).ToNot(ContainElements("fakeclustername"))
	})
})
