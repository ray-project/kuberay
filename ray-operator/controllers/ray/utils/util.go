package utils

import (
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	appsv1 "k8s.io/api/apps/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RayClusterSuffix = "-raycluster-"
)

// IsCreated returns true if pod has been created and is maintained by the API server
func IsCreated(pod *corev1.Pod) bool {
	return pod.Status.Phase != ""
}

// CheckName makes sure the name does not start with a numeric value and the total length is < 63 char
func CheckName(s string) string {
	maxLenght := 50 // 63 - (max(8,6) + 5 ) // 6 to 8 char are consumed at the end with "-head-" or -worker- + 5 generated.

	if len(s) > maxLenght {
		// shorten the name
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
		// shorten the name
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

// GenerateServiceName generates a ray head service name from cluster name
func GenerateServiceName(clusterName string) string {
	return fmt.Sprintf("%s-%s-%s", clusterName, rayiov1alpha1.HeadNode, "svc")
}

// GenerateIngressName generates an ingress name from cluster name
func GenerateIngressName(clusterName string) string {
	return fmt.Sprintf("%s-%s-%s", clusterName, rayiov1alpha1.HeadNode, "ingress")
}

// GenerateRayClusterName generates a ray cluster name from ray service name
func GenerateRayClusterName(serviceName string) string {
	return fmt.Sprintf("%s%s%s", serviceName, RayClusterSuffix, rand.String(5))
}

// GenerateIdentifier generates identifier of same group pods
func GenerateIdentifier(clusterName string, nodeType rayiov1alpha1.RayNodeType) string {
	return fmt.Sprintf("%s-%s", clusterName, nodeType)
}

// TODO: find target container through name instead of using index 0.
// FindRayContainerIndex finds the ray head/worker container's index in the pod
func FindRayContainerIndex(spec corev1.PodSpec) (index int) {
	// We only support one container at this moment. We definitely need a better way to filter out sidecar containers.
	if len(spec.Containers) > 1 {
		logrus.Warnf("Pod has multiple containers, we choose index=0 as Ray container")
	}
	return 0
}

// CalculateDesiredReplicas calculate desired worker replicas at the cluster level
func CalculateDesiredReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.Replicas
	}

	return count
}

// CalculateDesiredReplicas calculate desired worker replicas at the cluster level
func CalculateMinReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.MinReplicas
	}

	return count
}

// CalculateDesiredReplicas calculate desired worker replicas at the cluster level
func CalculateMaxReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.MaxReplicas
	}

	return count
}

// CalculateDesiredReplicas calculate desired worker replicas at the cluster level
func CalculateAvailableReplicas(pods corev1.PodList) int32 {
	count := int32(0)
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			count++
		}
	}

	return count
}

func Contains(s []string, searchTerm string) bool {
	i := sort.SearchStrings(s, searchTerm)
	return i < len(s) && s[i] == searchTerm
}

func FilterContainerByName(containers []corev1.Container, name string) (corev1.Container, error) {
	for _, container := range containers {
		if strings.Compare(container.Name, name) == 0 {
			return container, nil
		}
	}

	return corev1.Container{}, fmt.Errorf("can not find container %s", name)
}

// GetHeadGroupServiceAccountName returns the head group service account if it exists.
// Otherwise, it returns the name of the cluster itself.
func GetHeadGroupServiceAccountName(cluster *rayiov1alpha1.RayCluster) string {
	headGroupServiceAccountName := cluster.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName
	if headGroupServiceAccountName != "" {
		return headGroupServiceAccountName
	}
	return cluster.Name
}

// CheckAllPodsRunnning check if all pod in a list is running
func CheckAllPodsRunnning(runningPods corev1.PodList) bool {
	for _, pod := range runningPods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			return false
		}
	}
	return true
}

func PodNotMatchingTemplate(pod corev1.Pod, template corev1.PodTemplateSpec) bool {
	if pod.Status.Phase == corev1.PodRunning && pod.ObjectMeta.DeletionTimestamp == nil {
		if len(template.Spec.Containers) != len(pod.Spec.Containers) {
			return true
		}
		cmap := map[string]*corev1.Container{}
		for _, container := range pod.Spec.Containers {
			cmap[container.Name] = &container
		}
		for _, container1 := range template.Spec.Containers {
			if container2, ok := cmap[container1.Name]; ok {
				if container1.Image != container2.Image {
					// image name do not match
					return true
				}
				if len(container1.Resources.Requests) != len(container2.Resources.Requests) ||
					len(container1.Resources.Limits) != len(container2.Resources.Limits) {
					// resource entries do not match
					return true
				}

				resources1 := []corev1.ResourceList{
					container1.Resources.Requests,
					container1.Resources.Limits,
				}
				resources2 := []corev1.ResourceList{
					container2.Resources.Requests,
					container2.Resources.Limits,
				}
				for i := range resources1 {
					// we need to make sure all fields match
					for name, quantity1 := range resources1[i] {
						if quantity2, ok := resources2[i][name]; ok {
							if quantity1.Cmp(quantity2) != 0 {
								// request amount does not match
								return true
							}
						} else {
							// no such request
							return true
						}
					}
				}

				// now we consider them equal
				delete(cmap, container1.Name)
			} else {
				// container name do not match
				return true
			}
		}
		if len(cmap) != 0 {
			// one or more containers do not match
			return true
		}
	}
	return false
}

// GetEnvDefault return default environment value, if it's not found, defaultVal will be returned.
func GetEnvDefault(variable string, defaultVal string) string {
	envVar := os.Getenv(variable)
	if len(envVar) == 0 {
		return defaultVal
	}
	return envVar
}

// IgnoreNotFound provides a helper method to ignore (not requeue) NotFound errors
func IgnoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

// Reference: https://github.com/pwittrock/kubebuilder-workshop/blob/master/util/util.go
// CopyStatefulSetFields copies the owned fields from one StatefulSet to another
// Returns true if the fields copied from don't match to.
func CopyStatefulSetFields(from, to *appsv1.StatefulSet) bool {
	requireUpdate := false
	for k, v := range to.Labels {
		if from.Labels[k] != v {
			requireUpdate = true
		}
	}
	to.Labels = from.Labels

	for k, v := range to.Annotations {
		if from.Annotations[k] != v {
			requireUpdate = true
		}
	}
	to.Annotations = from.Annotations

	if from.Spec.Replicas != to.Spec.Replicas {
		to.Spec.Replicas = from.Spec.Replicas
		requireUpdate = true
	}

	if !reflect.DeepEqual(to.Spec.Template.Spec, from.Spec.Template.Spec) {
		requireUpdate = true
	}
	to.Spec.Template.Spec = from.Spec.Template.Spec

	return requireUpdate
}

// CopyServiceFields copies the owned fields from one Service to another
func CopyServiceFields(from, to *corev1.Service) bool {
	requireUpdate := false
	for k, v := range to.Labels {
		if from.Labels[k] != v {
			requireUpdate = true
		}
	}
	to.Labels = from.Labels

	for k, v := range to.Annotations {
		if from.Annotations[k] != v {
			requireUpdate = true
		}
	}
	to.Annotations = from.Annotations

	// Don't copy the entire Spec, because we can't overwrite the clusterIp field
	if !reflect.DeepEqual(to.Spec.Selector, from.Spec.Selector) {
		requireUpdate = true
	}
	to.Spec.Selector = from.Spec.Selector

	if !reflect.DeepEqual(to.Spec.Ports, from.Spec.Ports) {
		requireUpdate = true
	}
	to.Spec.Ports = from.Spec.Ports

	return requireUpdate
}
