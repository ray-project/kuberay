package common

import (
	"reflect"
	"testing"

	"github.com/ray-project/kuberay/ray-operator/controllers/raycluster/utils"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/raycluster/v1alpha1"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var instanceWithIngressEnabled = &rayiov1alpha1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
		Annotations: map[string]string{
			IngressClassAnnotationKey: "nginx",
		},
	},
	Spec: rayiov1alpha1.RayClusterSpec{
		RayVersion: "1.0",
		HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
			Replicas: pointer.Int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "ray-head",
							Image:   "rayproject/autoscaler",
							Command: []string{"python"},
							Args:    []string{"/opt/code.py"},
						},
					},
				},
			},
		},
	},
}

var instanceWithIngressEnabledWithoutIngressClass = &rayiov1alpha1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayiov1alpha1.RayClusterSpec{
		RayVersion: "1.0",
		HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
			Replicas: pointer.Int32Ptr(1),
			Template: corev1.PodTemplateSpec{
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

// only throw warning message and rely on Kubernetes to assign default ingress class
func TestBuildIngressForHeadServiceWithoutIngressClass(t *testing.T) {
	ingress, err := BuildIngressForHeadService(*instanceWithIngressEnabledWithoutIngressClass)
	assert.NotNil(t, ingress)
	assert.Nil(t, err)
}

func TestBuildIngressForHeadService(t *testing.T) {
	ingress, err := BuildIngressForHeadService(*instanceWithIngressEnabled)
	assert.Nil(t, err)

	// check ingress.class annotation
	actualResult := ingress.Labels[RayClusterLabelKey]
	expectedResult := instanceWithIngressEnabled.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualResult = ingress.Annotations[IngressClassAnnotationKey]
	expectedResult = instanceWithIngressEnabled.Annotations[IngressClassAnnotationKey]
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	// rules count
	assert.Equal(t, 1, len(ingress.Spec.Rules))

	// paths count
	expectedPaths := 1 // dashboard only
	actualPaths := len(ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths)
	if !reflect.DeepEqual(expectedPaths, actualPaths) {
		t.Fatalf("Expected `%v` but got `%v`", expectedPaths, actualPaths)
	}

	// path names
	paths := ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths
	for _, path := range paths {
		actualResult = path.Backend.Service.Name
		expectedResult = utils.GenerateServiceName(instanceWithIngressEnabled.Name)

		if !reflect.DeepEqual(expectedResult, actualResult) {
			t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
		}
	}
}
