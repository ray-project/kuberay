package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var instanceWithIngressEnabled = &rayv1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
		Annotations: map[string]string{
			IngressClassAnnotationKey: "nginx",
		},
	},
	Spec: rayv1.RayClusterSpec{
		HeadGroupSpec: rayv1.HeadGroupSpec{
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

var instanceWithIngressEnabledWithoutIngressClass = &rayv1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayv1.RayClusterSpec{
		HeadGroupSpec: rayv1.HeadGroupSpec{
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
	ingress, err := BuildIngressForHeadService(context.Background(), *instanceWithIngressEnabledWithoutIngressClass, "", []networkingv1.IngressTLS{}, map[string]string{})
	assert.NotNil(t, ingress)
	require.NoError(t, err)
}

func TestBuildIngressForHeadService(t *testing.T) {
	ingress, err := BuildIngressForHeadService(context.Background(), *instanceWithIngressEnabled, "", []networkingv1.IngressTLS{}, map[string]string{})
	require.NoError(t, err)

	// annotations count
	assert.Len(t, ingress.Annotations, 0)

	// check ingress.class annotation
	assert.Equal(t, instanceWithIngressEnabled.Name, ingress.Labels[utils.RayClusterLabelKey])

	// `annotations.kubernetes.io/ingress.class` was deprecated in Kubernetes 1.18,
	// and `spec.ingressClassName` is a replacement for this annotation. See
	// kubernetes.io/docs/concepts/services-networking/ingress/#deprecated-annotation
	// for more details.
	assert.Equal(t, "", ingress.Annotations[IngressClassAnnotationKey])

	assert.Equal(t, instanceWithIngressEnabled.Annotations[IngressClassAnnotationKey], *ingress.Spec.IngressClassName)

	// rules count
	assert.Len(t, ingress.Spec.Rules, 1)

	// paths count
	assert.Len(t, ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths, 1) // dashboard only

	// path names
	paths := ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths
	headSvcName, err := utils.GenerateHeadServiceName(utils.RayClusterCRD, instanceWithIngressEnabled.Spec, instanceWithIngressEnabled.Name)
	require.NoError(t, err)
	for _, path := range paths {
		assert.Equal(t, headSvcName, path.Backend.Service.Name)
	}

	// check host
	assert.Equal(t, ingress.Spec.Rules[0].Host, "")

	// tls count
	assert.Len(t, ingress.Spec.TLS, 0)
}

func TestBuildIngressForHeadServiceWithControllerConfigs(t *testing.T) {
	host := "ray-host"
	tls := []networkingv1.IngressTLS{
		{
			Hosts:      []string{"ray-host"},
			SecretName: "ray-tls-secret",
		},
	}
	ingressClass := "different-ingress-class"
	annotations := map[string]string{"arbitrary-annotation": "value", "another-annotation": "value2", IngressClassAnnotationKey: ingressClass}
	ingress, err := BuildIngressForHeadService(context.Background(), *instanceWithIngressEnabledWithoutIngressClass, host, tls, annotations)
	require.NoError(t, err)

	delete(annotations, IngressClassAnnotationKey)
	assert.Equal(t, ingress.Annotations, annotations)
	assert.Equal(t, *ingress.Spec.IngressClassName, ingressClass)
	assert.Equal(t, ingress.Spec.Rules[0].Host, "ray-host")
	assert.Equal(t, ingress.Spec.TLS, tls)
}

func TestBuildIngressForHeadServiceClusterSpecificAnnotationsTakePrecedence(t *testing.T) {
	annotations := map[string]string{"arbitrary-annotation": "value", "another-annotation": "value2", IngressClassAnnotationKey: "different-ingress-class"}
	ingress, err := BuildIngressForHeadService(context.Background(), *instanceWithIngressEnabled, "", []networkingv1.IngressTLS{}, annotations)
	require.NoError(t, err)

	delete(annotations, IngressClassAnnotationKey)
	assert.Equal(t, ingress.Annotations, annotations)
	assert.Equal(t, instanceWithIngressEnabled.Annotations[IngressClassAnnotationKey], *ingress.Spec.IngressClassName)
}
