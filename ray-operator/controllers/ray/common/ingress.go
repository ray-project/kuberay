package common

import (
	"fmt"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/sirupsen/logrus"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const IngressClassAnnotationKey = "kubernetes.io/ingress.class"

// BuildIngressForHeadService Builds the ingress for head service dashboard.
// This is used to expose dashboard and remote submit service apis for external traffic.
func BuildIngressForHeadService(cluster rayv1alpha1.RayCluster) (*networkingv1.Ingress, error) {
	labels := map[string]string{
		RayClusterLabelKey:                cluster.Name,
		RayIDLabelKey:                     utils.GenerateIdentifier(cluster.Name, rayv1alpha1.HeadNode),
		KubernetesApplicationNameLabelKey: ApplicationName,
		KubernetesCreatedByLabelKey:       ComponentName,
	}

	// Copy other ingress configurations from cluster annotations to provide a generic way
	// for user to customize their ingress settings. The `exclude_set` is used to avoid setting
	// both IngressClassAnnotationKey annotation which is deprecated and `Spec.IngressClassName`
	// at the same time.
	exclude_set := map[string]struct{}{
		IngressClassAnnotationKey: {},
	}
	annotation := map[string]string{}
	for key, value := range cluster.Annotations {
		if _, ok := exclude_set[key]; !ok {
			annotation[key] = value
		}
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
			Path:     "/" + cluster.Name + "/(.*)",
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
			Name:        utils.GenerateIngressName(cluster.Name),
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
		// TODO: in AWS EKS, set up IngressClassName will cause an error due to conflict with annotation.
		ingress.Spec.IngressClassName = &ingressClassName
	}

	return ingress, nil
}

// BuildIngressForRayService Builds the ingress for head service dashboard for RayService.
// This is used to expose dashboard for external traffic.
// RayService controller updates the ingress whenever a new RayCluster serves the traffic.
func BuildIngressForRayService(service rayv1alpha1.RayService, cluster rayv1alpha1.RayCluster) (*networkingv1.Ingress, error) {
	ingress, err := BuildIngressForHeadService(cluster)
	if err != nil {
		return nil, err
	}

	ingress.ObjectMeta.Name = utils.GenerateServiceName(service.Name)
	ingress.ObjectMeta.Namespace = service.Namespace
	ingress.ObjectMeta.Labels = map[string]string{
		RayServiceLabelKey: service.Name,
		RayIDLabelKey:      utils.CheckLabel(utils.GenerateIdentifier(service.Name, rayv1alpha1.HeadNode)),
	}

	return ingress, nil
}
