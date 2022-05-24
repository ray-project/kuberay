package common

import (
	"fmt"

	"github.com/ray-project/kuberay/ray-operator/controllers/raycluster/utils"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/raycluster/v1alpha1"
	"github.com/sirupsen/logrus"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const IngressClassAnnotationKey = "kubernetes.io/ingress.class"

// BuildIngressForHeadService Builds the ingress for head service dashboard.
// This is used to expose dashboard for external traffic.
func BuildIngressForHeadService(cluster rayiov1alpha1.RayCluster) (*networkingv1.Ingress, error) {
	labels := map[string]string{
		RayClusterLabelKey: cluster.Name,
		RayIDLabelKey:      utils.GenerateIdentifier(cluster.Name, rayiov1alpha1.HeadNode),
	}

	// Copy other ingress configuration from cluster annotation
	// This is to provide a generic way for user to customize their ingress settings.
	annotation := map[string]string{}
	for key, value := range cluster.Annotations {
		annotation[key] = value
	}

	var paths []networkingv1.HTTPIngressPath
	pathType := networkingv1.PathTypeExact
	servicePorts := getServicePorts(cluster)
	dashboardPort := int32(DefaultDashboardPort)
	if port, ok := servicePorts["dashboard"]; ok {
		dashboardPort = port
	}
	paths = []networkingv1.HTTPIngressPath{
		{
			Path:     "/" + cluster.Name,
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: utils.GenerateServiceName(cluster.Name),
					Port: networkingv1.ServiceBackendPort{
						Number: dashboardPort,
					},
				},
			},
		},
	}

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateServiceName(cluster.Name),
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotation,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: paths,
						},
					},
				},
			},
		},
	}

	// Get ingress class name from rayCluster annotations. this is a required field to use ingress.
	ingressClassName, ok := cluster.Annotations[IngressClassAnnotationKey]
	if !ok {
		logrus.Warn(fmt.Sprintf("ingress class annotation is not set for cluster %s/%s", cluster.Namespace, cluster.Name))
	} else {
		ingress.Spec.IngressClassName = &ingressClassName
	}

	return ingress, nil
}
