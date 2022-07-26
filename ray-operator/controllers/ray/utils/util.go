package utils

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"k8s.io/apimachinery/pkg/util/json"

	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/sirupsen/logrus"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RayClusterSuffix = "-raycluster-"
	DashboardName    = "dashboard"
	ServeName        = "serve"
)

// IsCreated returns true if pod has been created and is maintained by the API server
func IsCreated(pod *corev1.Pod) bool {
	return pod.Status.Phase != ""
}

// IsRunningAndReady returns true if pod is in the PodRunning Phase, if it has a condition of PodReady.
func IsRunningAndReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
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

// GenerateDashboardServiceName generates a ray head service name from cluster name
func GenerateDashboardServiceName(clusterName string) string {
	return fmt.Sprintf("%s-%s-%s", clusterName, DashboardName, "svc")
}

// GenerateDashboardAgentLabel generates label value for agent service selector.
func GenerateDashboardAgentLabel(clusterName string) string {
	return fmt.Sprintf("%s-%s", clusterName, DashboardName)
}

// GenerateServeServiceName generates name for serve service.
func GenerateServeServiceName(serviceName string) string {
	return fmt.Sprintf("%s-%s-%s", serviceName, ServeName, "svc")
}

// GenerateServeServiceLabel generates label value for serve service selector.
func GenerateServeServiceLabel(serviceName string) string {
	return fmt.Sprintf("%s-%s", serviceName, ServeName)
}

// GenerateIngressName generates an ingress name from cluster name
func GenerateIngressName(clusterName string) string {
	return fmt.Sprintf("%s-%s-%s", clusterName, rayiov1alpha1.HeadNode, "ingress")
}

// GenerateRayClusterName generates a ray cluster name from ray service name
func GenerateRayClusterName(serviceName string) string {
	return fmt.Sprintf("%s%s%s", serviceName, RayClusterSuffix, rand.String(5))
}

// GenerateRayJobId generates a ray job id for submission
func GenerateRayJobId(rayjob string) string {
	return fmt.Sprintf("%s-%s", rayjob, rand.String(5))
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
	// check if there is no pods.
	if len(runningPods.Items) == 0 {
		return false
	}
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

// CompareJsonStruct This is a way to better compare if two objects are the same when they are json/yaml structs. reflect.DeepEqual will fail in some cases.
func CompareJsonStruct(objA interface{}, objB interface{}) bool {
	a, err := json.Marshal(objA)
	if err != nil {
		return false
	}
	b, err := json.Marshal(objB)
	if err != nil {
		return false
	}
	var v1, v2 interface{}
	err = json.Unmarshal(a, &v1)
	if err != nil {
		return false
	}
	err = json.Unmarshal(b, &v2)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(v1, v2)
}

func ConvertUnixTimeToMetav1Time(unixTime int64) *metav1.Time {
	t := time.Unix(unixTime, 0)
	kt := metav1.NewTime(t)
	return &kt
}
