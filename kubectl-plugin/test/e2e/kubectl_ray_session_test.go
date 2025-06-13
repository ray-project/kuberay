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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Calling ray plugin `session` command", Ordered, func() {
	var namespace string
	var testClient Client

	BeforeEach(func() {
		namespace = createTestNamespace()

		var err error
		testClient, err = newTestClient()
		Expect(err).NotTo(HaveOccurred())

		deployTestRayCluster(namespace)
		DeferCleanup(func() {
			deleteTestNamespace(namespace)
			namespace = ""
		})
	})

	It("succeed in forwarding RayCluster and should be able to cancel", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, "kubectl", "ray", "session", "--namespace", namespace, "raycluster-kuberay")
		// Set the command to run in a new process group
		// This is necessary to ensure that the signal is sent to all child processes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		err := cmd.Start()
		Expect(err).NotTo(HaveOccurred())
		// Make sure we kill the whole process group, even if the test is failure
		defer cleanUpCmdProcessAndCheckPortForwarding(cmd)

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.CommandContext(ctx, "curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 3*time.Second, 500*time.Millisecond).ShouldNot(HaveOccurred())
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		Expect(err).NotTo(HaveOccurred())

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
			Fail("kubectl ray session did not terminate after SIGINT")
		}
	})

	It("should reconnect after pod connection is lost", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sessionCmd := exec.CommandContext(ctx, "kubectl", "ray", "session", "--namespace", namespace, "raycluster-kuberay")
		// Set the command to run in a new process group
		// This is necessary to ensure that the signal is sent to all child processes
		sessionCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		err := sessionCmd.Start()
		Expect(err).NotTo(HaveOccurred())

		// Make sure we kill the whole process group, even if the test is failure
		defer cleanUpCmdProcessAndCheckPortForwarding(sessionCmd)

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.CommandContext(ctx, "curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 3*time.Second, 500*time.Millisecond).ShouldNot(HaveOccurred())

		// Get the current head pod name
		oldPod, err := testClient.Core().CoreV1().Pods(namespace).Get(ctx, "raycluster-kuberay-head", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		oldPodUID := string(oldPod.UID)
		var newPodUID string

		// Delete the pod
		err = testClient.Core().CoreV1().Pods(namespace).Delete(ctx, "raycluster-kuberay-head", metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Wait for the new pod to be created
		Eventually(func() error {
			newPod, err := testClient.Core().CoreV1().Pods(namespace).Get(ctx, "raycluster-kuberay-head", metav1.GetOptions{})
			newPodUID = string(newPod.UID)
			if err != nil {
				return err
			}
			if newPodUID == oldPodUID {
				return fmt.Errorf("head pod has not changed (UID still %s)", oldPodUID)
			}
			return nil
		}, 60*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

		// Wait for the new pod to be ready
		cmd := exec.Command("kubectl", "wait", "--namespace", namespace, "pod/raycluster-kuberay-head", "--for=condition=Ready", "--timeout=120s")
		err = cmd.Run()
		Expect(err).NotTo(HaveOccurred())

		// Send a request to localhost:8265, it should succeed
		Eventually(func() error {
			_, err := exec.CommandContext(ctx, "curl", "http://localhost:8265").CombinedOutput()
			return err
		}, 60*time.Second, 1*time.Millisecond).ShouldNot(HaveOccurred())
	})

	It("should not succeed", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, "kubectl", "ray", "session", "--namespace", namespace, "fakeclustername")
		output, err := cmd.CombinedOutput()

		Expect(err).To(HaveOccurred())
		Expect(output).ToNot(ContainElements("fakeclustername"))
	})
})

func cleanUpCmdProcessAndCheckPortForwarding(cmd *exec.Cmd) {
	pgid, _ := syscall.Getpgid(cmd.Process.Pid)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	// prevent zombie process
	_ = cmd.Wait()
	// Check if the port-forwarding process is still running
	_, err := exec.Command("lsof", "-i", ":8265").CombinedOutput()
	Expect(err).To(HaveOccurred())
}
