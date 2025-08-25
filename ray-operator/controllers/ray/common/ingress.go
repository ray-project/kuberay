package common

import (
	"bytes"
	"context"
	"text/template"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	IngressClassAnnotationKey = "kubernetes.io/ingress.class"

	IngressHostKey     = "raycluster.ray.io/ingress.host"
	IngressPathKey     = "raycluster.ray.io/ingress.path"
	IngressPathTypeKey = "raycluster.ray.io/ingress.path-type"
)

// BuildIngressForHeadService Builds the ingress for head service dashboard.
// This is used to expose dashboard for external traffic.
func BuildIngressForHeadService(ctx context.Context, cluster rayv1.RayCluster) (*networkingv1.Ingress, error) {
	log := ctrl.LoggerFrom(ctx)

	labels := map[string]string{
		utils.RayClusterLabelKey:                cluster.Name,
		utils.RayIDLabelKey:                     utils.CheckLabel(utils.GenerateIdentifier(cluster.Name, rayv1.HeadNode)),
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}

	// Copy other ingress configurations from cluster annotations to provide a generic way
	// for user to customize their ingress settings. The `excludeSet` is used to avoid setting
	// both IngressClassAnnotationKey annotation which is deprecated and `Spec.IngressClassName`
	// at the same time.
	excludeSet := map[string]struct{}{
		IngressClassAnnotationKey: {},
	}
	annotation := map[string]string{}
	for key, value := range cluster.Annotations {
		if _, ok := excludeSet[key]; !ok {
			annotation[key] = value
		}
	}

	pathType := networkingv1.PathTypeExact
	if pathTypeAnnotation, ok := annotation[IngressPathTypeKey]; ok {
		pathType = networkingv1.PathType(pathTypeAnnotation)
	}
	host, path, err := renderAnnotations(annotation, ingressAnnotationVars{
		ClusterName: cluster.Name,
	})
	if err != nil {
		log.Info("Ingress host and path annotation cannot be set.", "clusterNamespace", cluster.Namespace,
			"clusterName", cluster.Name, "error", err)
		host = ""
		path = "/" + cluster.Name + "/(.*)"
	}

	servicePorts := getServicePorts(cluster)
	dashboardPort := int32(utils.DefaultDashboardPort)
	if port, ok := servicePorts["dashboard"]; ok {
		dashboardPort = port
	}

	headSvcName, err := utils.GenerateHeadServiceName(utils.RayClusterCRD, cluster.Spec, cluster.Name)
	if err != nil {
		return nil, err
	}
	paths := []networkingv1.HTTPIngressPath{
		{
			Path:     path,
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: headSvcName,
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
					Host: host,
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
	if ingressClassName, ok := cluster.Annotations[IngressClassAnnotationKey]; !ok {
		log.Info("Ingress class annotation is not set for the cluster.", "clusterNamespace", cluster.Namespace, "clusterName", cluster.Name)
	} else {
		// TODO: in AWS EKS, set up IngressClassName will cause an error due to conflict with annotation.
		ingress.Spec.IngressClassName = &ingressClassName
	}

	return ingress, nil
}

type ingressAnnotationVars struct {
	ClusterName string
}

func renderAnnotations(annotations map[string]string, variables ingressAnnotationVars) (host, path string, _ error) {
	if hostAnnot, ok := annotations[IngressHostKey]; ok {
		newHost, err := render(hostAnnot, variables)
		if err != nil {
			return "", "", err
		}

		host = newHost
	}

	if pathAnnot, ok := annotations[IngressPathKey]; ok {
		newPath, err := render(pathAnnot, variables)
		if err != nil {
			return "", "", err
		}

		path = newPath
	}

	return
}

func render(tmpl string, variables ingressAnnotationVars) (string, error) {
	t, err := template.New("ingress").Parse(tmpl)
	if err != nil {
		return "", err
	}

	result := bytes.Buffer{}
	err = t.Execute(&result, variables)
	if err != nil {
		return "", err
	}

	return result.String(), nil
}
