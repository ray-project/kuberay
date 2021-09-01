package utils

import (
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	validation "k8s.io/apimachinery/pkg/util/validation"
)

const DashSymbol = "-"

// IsValid returns true if name is valid
func IsValid(name string) (errorList []string, notValid bool) {
	return validation.IsQualifiedName(name), len(validation.IsQualifiedName(name)) == 0
}

// trimName shortens a name
func TrimName(name string) string {
	// leaving 8 extra characters up to the max allowed = 63
	maxLength := 55
	if len(name) > maxLength {
		name = name[:maxLength]
	}
	name = strings.TrimSpace(name)
	// remove last trailing dash(s)
	for len(name) > 0 && name[len(name)-1:] == DashSymbol {
		name = strings.TrimSuffix(name, DashSymbol)
	}
	// remove dash(s) at the begining
	for len(name) > 0 && name[0:1] == DashSymbol {
		name = strings.TrimPrefix(name, DashSymbol)
	}
	return name
}

func TrimMap(myMap map[string]string) map[string]string {
	if myMap == nil {
		return myMap
	}
	for key, element := range myMap {
		myMap[key] = TrimName(element)
	}
	return myMap
}

// IsCreated returns true if pod has been created and is maintained by the API server
func IsCreated(pod *corev1.Pod) bool {
	return pod.Status.Phase != ""
}

// Get substring before a string.
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
