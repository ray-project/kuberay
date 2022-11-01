package common

import (
	"reflect"
	"testing"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var instanceWithWrongSvc = &rayiov1alpha1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayiov1alpha1.RayClusterSpec{
		RayVersion: "1.0",
		HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
			Replicas: pointer.Int32Ptr(1),
			RayStartParams: map[string]string{
				"port":                "6379",
				"object-manager-port": "12345",
				"node-manager-port":   "12346",
				"object-store-memory": "100000000",
				"redis-password":      "LetMeInRay",
				"num-cpus":            "1",
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						"rayCluster": "raycluster-sample",
						"groupName":  "headgroup",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "ray-head",
							Image:   "rayproject/autoscaler",
							Command: []string{"python"},
							Args:    []string{"/opt/code.py"},
							Env: []corev1.EnvVar{
								{
									Name: "MY_POD_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	},
}

func TestBuildServiceForHeadPod(t *testing.T) {
	svc, err := BuildServiceForHeadPod(*instanceWithWrongSvc, nil)
	assert.Nil(t, err)

	actualResult := svc.Spec.Selector[RayClusterLabelKey]
	expectedResult := string(instanceWithWrongSvc.Name)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualResult = svc.Spec.Selector[RayNodeTypeLabelKey]
	expectedResult = string(rayiov1alpha1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualResult = svc.Spec.Selector[KubernetesApplicationNameLabelKey]
	expectedResult = ApplicationName
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	ports := svc.Spec.Ports
	expectedResult = DefaultServiceAppProtocol
	for _, port := range ports {
		if *port.AppProtocol != DefaultServiceAppProtocol {
			t.Fatalf("Expected `%v` but got `%v`", expectedResult, *port.AppProtocol)
		}
	}
}

func TestBuildServiceForHeadPodWithAppNameLabel(t *testing.T) {
	labels := make(map[string]string)
	labels[KubernetesApplicationNameLabelKey] = "testname"
	svc, err := BuildServiceForHeadPod(*instanceWithWrongSvc, labels)
	assert.Nil(t, err)

	actualResult := svc.Spec.Selector[KubernetesApplicationNameLabelKey]
	expectedResult := "testname"
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualLength := len(svc.Spec.Selector)
	// We have 5 default labels in `BuildServiceForHeadPod`, and `KubernetesApplicationNameLabelKey`
	// is one of the default labels. Hence, `expectedLength` should also be 5.
	expectedLength := 5
	if actualLength != expectedLength {
		t.Fatalf("Expected `%v` but got `%v`", expectedLength, actualLength)
	}
}
