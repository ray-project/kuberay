package common

import (
	"errors"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/utils"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const IngressClassAnnotationKey = "kubernetes.io/ingress.class"

// BuildIngressForHeadService Builds the ingress for head service.
// This is used to expose external an endpoint for external traffic.
func BuildIngressForHeadService(cluster rayiov1alpha1.RayCluster) (*networkingv1.Ingress, error) {
	labels := map[string]string{
		RayClusterLabelKey: cluster.Name,
		RayIDLabelKey:      utils.GenerateIdentifier(cluster.Name, rayiov1alpha1.HeadNode),
	}

	// Get ingress class name from rayCluster annotations. this is a required field to use ingress.
	_, ok := cluster.Annotations[IngressClassAnnotationKey]
	if !ok {
		return nil, errors.New("can not find ingress class name from annotation")
	}

	// Copy other ingress configuration from cluster annotation
	// This is to provide a generic way for user to customize their ingress settings.
	annotation := map[string]string{}
	for key, value := range cluster.Annotations {
		annotation[key] = value
	}

	// The tricky thing is we need three ports. redis, dashboard, client. should we use /dashboard /redis to support that?
	// 1. does ingress support non-UI ports like 10001, 6379 etc.
	// 2. verify ingress controller.

	var paths []networkingv1.HTTPIngressPath
	pathType := networkingv1.PathTypeExact
	serviceName := utils.GenerateServiceName(cluster.Name)
	ports := getServicePorts(cluster)
	for portName, port := range ports {
		paths = append(paths, networkingv1.HTTPIngressPath{
			Path:     "/" + portName,
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: serviceName,
					Port: networkingv1.ServiceBackendPort{
						Number: port,
					},
				},
			},
		})
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
							// https://github.com/kubernetes/ingress-nginx/issues/1655#issuecomment-79129310
							// It's ok to expose more ports on the same path
							Paths: paths,
						},
					},
				},
			},
		},
	}

	return ingress, nil
}
