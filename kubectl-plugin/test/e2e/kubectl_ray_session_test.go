package e2e

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Calling ray plugin `session` command", Ordered, func() {
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "kubectl", "ray", "session", "--namespace", namespace, "raycluster-kuberay")
		// Set the command to run in a new process group
		// This is necessary to ensure that the signal is sent to all child processes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		err := cmd.Start()
		Expect(err).NotTo(HaveOccurred())

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.CommandContext(ctx, "curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 3*time.Second, 500*time.Millisecond).ShouldNot(HaveOccurred())

		// Send a signal to cancel the command
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		Expect(err).NotTo(HaveOccurred())
		// Make sure we kill the whole process group, even if the test is failure
		defer func() {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			// prevent zombie process
			_ = cmd.Wait()
		}()

		// Send Interrupt signal to the process group
		// This will also send the signal to all child processes
		err = syscall.Kill(-pgid, syscall.SIGINT)
		Expect(err).NotTo(HaveOccurred())

		select {
		case err = <-done:
			if err != nil {
				exitErr := &exec.ExitError{}
				Expect(errors.As(err, &exitErr)).To(BeTrue())
				ws := exitErr.Sys().(syscall.WaitStatus)
				Expect(ws.Signal()).To(Equal(syscall.SIGINT))
			}
		case <-time.After(3 * time.Second):
			// force kill the process if it doesn't exit within the timeout
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			Fail("kubectl ray session did not terminate after SIGINT")
		}
		// Check if the port-forwarding process is still running
		_, err = exec.CommandContext(ctx, "lsof", "-i", ":8265").CombinedOutput()
		Expect(err).To(HaveOccurred())
	})

	It("should reconnect after pod connection is lost", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		sessionCmd := exec.CommandContext(ctx, "kubectl", "ray", "session", "--namespace", namespace, "raycluster-kuberay")

		err := sessionCmd.Start()
		Expect(err).NotTo(HaveOccurred())

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.CommandContext(ctx, "curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 3*time.Second, 500*time.Millisecond).ShouldNot(HaveOccurred())

		// Get the current head pod name
		cmd := exec.CommandContext(ctx, "kubectl", "get", "--namespace", namespace, "pod/raycluster-kuberay-head", "-o", "jsonpath={.metadata.uid}")
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred())
		oldPodUID := string(output)
		var newPodUID string

		// Delete the pod
		cmd = exec.CommandContext(ctx, "kubectl", "delete", "--namespace", namespace, "pod/raycluster-kuberay-head")
		err = cmd.Run()
		Expect(err).NotTo(HaveOccurred())

		// Wait for the new pod to be created
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "get", "--namespace", namespace, "pod/raycluster-kuberay-head", "-o", "jsonpath={.metadata.uid}")
			output, err := cmd.CombinedOutput()
			newPodUID = string(output)
			if err != nil {
				return err
			}
			if newPodUID == oldPodUID {
				return fmt.Errorf("head pod has not changed (UID still %s)", oldPodUID)
			}
			return nil
		}, 60*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

		// Wait for the new pod to be ready
		cmd = exec.CommandContext(ctx, "kubectl", "wait", "--namespace", namespace, "pod/raycluster-kuberay-head", "--for=condition=Ready", "--timeout=120s")
		err = cmd.Run()
		Expect(err).NotTo(HaveOccurred())

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.CommandContext(ctx, "curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 60*time.Second, 1*time.Millisecond).ShouldNot(HaveOccurred())

		err = sessionCmd.Process.Kill()
		Expect(err).NotTo(HaveOccurred())
		_ = sessionCmd.Wait()
	})

	It("should not succeed", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "kubectl", "ray", "session", "--namespace", namespace, "fakeclustername")
		output, err := cmd.CombinedOutput()

		Expect(err).To(HaveOccurred())
		Expect(output).ToNot(ContainElements("fakeclustername"))
	})
})
