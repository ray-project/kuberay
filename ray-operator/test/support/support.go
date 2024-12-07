package support

import (
	"fmt"
	"os"
	"time"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
