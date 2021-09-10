package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IsCreated returns true if pod has been created and is maintained by the API server
func IsCreated(pod *corev1.Pod) bool {
	return pod.Status.Phase != ""
}

// CheckName makes sure the name does not start with a numeric value and the total length is < 63 char
func CheckName(s string) string {
	maxLenght := 50 // 63 - (max(8,6) + 5 ) // 6 to 8 char are consumed at the end with "-head-" or -worker- + 5 generated.

	if len(s) > maxLenght {
		//shorten the name
		offset := int(math.Abs(float64(maxLenght) - float64(len(s))))
		fmt.Printf("pod name is too long: len = %v, we will shorten it by offset = %v\n", len(s), offset)
		s = s[offset:]
	}

	// cannot start with a numeric value
	if unicode.IsDigit(rune(s[0])) {
		s = "r" + s[1:]
	}

	// cannot start with a punctuation
	if unicode.IsPunct(rune(s[0])) {
		fmt.Println(s)
		s = "r" + s[1:]
	}

	return s
}

// CheckLabel makes sure the label value does not start with a punctuation and the total length is < 63 char
func CheckLabel(s string) string {
	maxLenght := 63

	if len(s) > maxLenght {
		//shorten the name
		offset := int(math.Abs(float64(maxLenght) - float64(len(s))))
		fmt.Printf("label value is too long: len = %v, we will shorten it by offset = %v\n", len(s), offset)
		s = s[offset:]
	}

	// cannot start with a punctuation
	if unicode.IsPunct(rune(s[0])) {
		fmt.Println(s)
		s = "r" + s[1:]
	}

	return s
}

// SetHeadSelector makes sure the selector is correct
func SetHeadSelector(rayPodSvc *corev1.Service, rayClusterName string) {
	if rayPodSvc.Spec.Selector == nil {
		rayPodSvc.Spec.Selector = make(map[string]string)
	}
	if _, ok := rayPodSvc.Spec.Selector["identifier"]; !ok {
		rayPodSvc.Spec.Selector["identifier"] = CheckLabel(fmt.Sprintf("%s-%s", rayClusterName, "head"))
	}
	if rayPodSvc.Spec.Selector["identifier"] != CheckLabel(fmt.Sprintf("%s-%s", rayClusterName, "head")) {
		rayPodSvc.Spec.Selector["identifier"] = CheckLabel(fmt.Sprintf("%s-%s", rayClusterName, "head"))
	}
}

// Before Get substring before a string.
func Before(value string, a string) string {
	pos := strings.Index(value, a)
	if pos == -1 {
		return ""
	}
	return value[0:pos]
}

// FormatInt returns the string representation of i in the given base,
// for 2 <= base <= 36. The result uses the lower-case letters 'a' to 'z'
// for digit values >= 10.
func FormatInt32(n int32) string {
	return strconv.FormatInt(int64(n), 10)
}

// GetNamespace return namespace
func GetNamespace(metaData metav1.ObjectMeta) string {
	if metaData.Namespace == "" {
		return "default"
	}
	return metaData.Namespace
}
