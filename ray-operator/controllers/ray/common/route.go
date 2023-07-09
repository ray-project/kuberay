package common

import (
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	routev1 "github.com/openshift/api/route/v1"
	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// BuildRouteForHeadService Builds the Route (OpenShift) for head service dashboard.
// This is used to expose dashboard and ray job submission API for external traffic.
func BuildRouteForHeadService(cluster rayv1alpha1.RayCluster) (*routev1.Route, error) {
	labels := map[string]string{
		RayClusterLabelKey:                cluster.Name,
		RayIDLabelKey:                     utils.GenerateIdentifier(cluster.Name, rayv1alpha1.HeadNode),
		KubernetesApplicationNameLabelKey: ApplicationName,
		KubernetesCreatedByLabelKey:       ComponentName,
	}

	// Copy other route configurations from cluster annotations to provide a generic way
	// for user to customize their route settings. 
	annotation := map[string]string{}
	for key, value := range cluster.Annotations {
		annotation[key] = value
	}

	servicePorts := getServicePorts(cluster)
	dashboardPort := DefaultDashboardPort
	if port, ok := servicePorts[DefaultDashboardName]; ok {
		dashboardPort = int(port)
	}

	weight := int32(100)

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateRouteName(cluster.Name),
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotation,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   utils.GenerateServiceName(cluster.Name),
				Weight: &weight,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt(dashboardPort),
			},
			WildcardPolicy: "None",
		},
	}

	return route, nil
}

// BuildRouteForRayService Builds the ingress for head service dashboard for RayService.
// This is used to expose dashboard for external traffic.
// RayService controller updates the ingress whenever a new RayCluster serves the traffic.
func BuildRouteForRayService(service rayv1alpha1.RayService, cluster rayv1alpha1.RayCluster) (*routev1.Route, error) {
	route, err := BuildRouteForHeadService(cluster)
	if err != nil {
		return nil, err
	}

	route.ObjectMeta.Name = utils.GenerateServiceName(service.Name)
	route.ObjectMeta.Namespace = service.Namespace
	route.ObjectMeta.Labels[RayServiceLabelKey] = service.Name
	route.ObjectMeta.Labels[RayIDLabelKey] = utils.CheckLabel(utils.GenerateIdentifier(service.Name, rayv1alpha1.HeadNode))

	return route, nil
}
