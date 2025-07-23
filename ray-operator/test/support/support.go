package support

import (
	"fmt"
	"os"
	"time"

	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

var (
	TestApplyOptions = metav1.ApplyOptions{FieldManager: "kuberay-test", Force: true}

	TestTimeoutShort  = 1 * time.Minute
	TestTimeoutMedium = 2 * time.Minute
	TestTimeoutLong   = 5 * time.Minute
)

func init() {
	if value, ok := os.LookupEnv("KUBERAY_TEST_TIMEOUT_SHORT"); ok {
		if duration, err := time.ParseDuration(value); err == nil {
			TestTimeoutShort = duration
		} else {
			fmt.Printf("Error parsing KUBERAY_TEST_TIMEOUT_SHORT. Using default value: %s", TestTimeoutShort)
		}
	}
	if value, ok := os.LookupEnv("KUBERAY_TEST_TIMEOUT_MEDIUM"); ok {
		if duration, err := time.ParseDuration(value); err == nil {
			TestTimeoutMedium = duration
		} else {
			fmt.Printf("Error parsing KUBERAY_TEST_TIMEOUT_MEDIUM. Using default value: %s", TestTimeoutMedium)
		}
	}
	if value, ok := os.LookupEnv("KUBERAY_TEST_TIMEOUT_LONG"); ok {
		if duration, err := time.ParseDuration(value); err == nil {
			TestTimeoutLong = duration
		} else {
			fmt.Printf("Error parsing KUBERAY_TEST_TIMEOUT_LONG. Using default value: %s", TestTimeoutLong)
		}
	}

	// Gomega settings
	gomega.SetDefaultEventuallyTimeout(TestTimeoutShort)
	gomega.SetDefaultEventuallyPollingInterval(1 * time.Second)
	gomega.SetDefaultConsistentlyDuration(30 * time.Second)
	gomega.SetDefaultConsistentlyPollingInterval(1 * time.Second)
	// Disable object truncation on test results
	format.MaxLength = 0
}

func IsPodRunningAndReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func AllPodsRunningAndReady(pods []corev1.Pod) bool {
	for _, pod := range pods {
		if !IsPodRunningAndReady(&pod) {
			return false
		}
	}
	return true
}

func DeletePodAndWait(test Test, rayCluster *rayv1.RayCluster, namespace *corev1.Namespace, currentHeadPod *corev1.Pod) (*corev1.Pod, error) {
	g := NewWithT(test.T())

	err := test.Client().Core().CoreV1().Pods(namespace.Name).Delete(test.Ctx(), currentHeadPod.Name, metav1.DeleteOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to delete head pod %s: %w", currentHeadPod.Name, err)
	}

	PodUID := func(p *corev1.Pod) string { return string(p.UID) }

	// Wait for a new head pod to be created (different UID)
	g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
		ShouldNot(WithTransform(PodUID, Equal(string(currentHeadPod.UID))),
			"New head pod should have different UID than the deleted one")

	g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
		Should(WithTransform(func(p *corev1.Pod) string { return string(p.Status.Phase) }, Equal("Running")),
			"New head pod should be in Running state")

	newHeadPod, err := GetHeadPod(test, rayCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get new head pod: %w", err)
	}

	return newHeadPod, nil
}
