package common

import (
	"errors"
	"strings"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	routev1 "github.com/openshift/api/route/v1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// BuildRouteForHeadService Builds the Route (OpenShift) for head service dashboard.
// This is used to expose dashboard and remote submit service apis or external traffic.
func BuildRouteForHeadService(cluster rayv1.RayCluster) ([]*routev1.Route, error) {
	var routeList []*routev1.Route
	labels := map[string]string{
		utils.RayClusterLabelKey:                cluster.Name,
		utils.RayIDLabelKey:                     utils.GenerateIdentifier(cluster.Name, rayv1.HeadNode),
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}
	weight := int32(100)

	serviceName, err := utils.GenerateHeadServiceName("RayCluster", cluster.Spec, cluster.Name)
	if err != nil {
		return nil, err
	}

	ingressOptionsList := cluster.Spec.HeadGroupSpec.IngressOptions.Ingresses
	if len(ingressOptionsList) == 0 {
		// Copy other configurations from cluster annotations to provide a generic way
		// for user to customize their route settings.
		annotation := map[string]string{}
		for key, value := range cluster.Annotations {
			annotation[key] = value
		}

		servicePorts := getServicePorts(cluster)
		dashboardPort := DefaultDashboardPort
		if port, ok := servicePorts["dashboard"]; ok {
			dashboardPort = int(port)
		}

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
					Name:   serviceName,
					Weight: &weight,
				},
				Port: &routev1.RoutePort{
					TargetPort: intstr.FromInt(dashboardPort),
				},
				WildcardPolicy: "None",
			},
		}
		routeList = append(routeList, route)
		return routeList, nil
	} else {
		for _, ingressItem := range ingressOptionsList {
			annotations := ingressItem.Annotations
			tlsConfigs := ingressItem.TLSConfig
			port := ingressItem.Port
			annotation := map[string]string{}
			tlsConfig := map[string]string{}

			for key, value := range annotations {
				annotation[key] = value
			}

			for key, value := range tlsConfigs {
				tlsConfig[key] = value
			}

			route := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:        ingressItem.IngressName,
					Annotations: annotation,
					Namespace:   cluster.Namespace,
					Labels:      labels,
				},
				Spec: routev1.RouteSpec{
					To: routev1.RouteTargetReference{
						Kind:   "Service",
						Name:   serviceName,
						Weight: &weight,
					},
					Port: &routev1.RoutePort{
						TargetPort: intstr.FromInt(port),
					},
					WildcardPolicy: "None",
				},
			}
			if len(annotation) > 0 {
				route.ObjectMeta.Annotations = annotation
			}
			if len(tlsConfig) > 0 {
				var termination routev1.TLSTerminationType
				var insecureEdgePolicyType routev1.InsecureEdgeTerminationPolicyType
				switch strings.ToLower(tlsConfig["termination"]) {
				case "passthrough":
					termination = routev1.TLSTerminationPassthrough
				case "edge":
					termination = routev1.TLSTerminationEdge
				case "reencrypt":
					termination = routev1.TLSTerminationReencrypt
				default:
					return routeList, errors.New("Termination value not found please specify one of the following passthrough, edge, reencrypt")
				}

				_, exists := tlsConfig["insecureEdgePolicy"]
				if exists {
					switch strings.ToLower(tlsConfig["insecureEdgePolicy"]) {
					case "none":
						insecureEdgePolicyType = routev1.InsecureEdgeTerminationPolicyNone
					case "allow":
						insecureEdgePolicyType = routev1.InsecureEdgeTerminationPolicyAllow
					case "redirect":
						insecureEdgePolicyType = routev1.InsecureEdgeTerminationPolicyRedirect
					default:
						insecureEdgePolicyType = routev1.InsecureEdgeTerminationPolicyNone
					}
				} else {
					insecureEdgePolicyType = routev1.InsecureEdgeTerminationPolicyNone
				}

				route.Spec.TLS = &routev1.TLSConfig{
					Termination:                   termination,
					InsecureEdgeTerminationPolicy: insecureEdgePolicyType,
				}

			}
			routeList = append(routeList, route)
		}
		return routeList, nil
	}
}

// BuildRouteForRayService Builds the route for head service dashboard for RayService.
// This is used to expose dashboard for external traffic.
// RayService controller updates the ingress whenever a new RayCluster serves the traffic.
func BuildRouteForRayService(service rayv1.RayService, cluster rayv1.RayCluster) (*routev1.Route, error) {
	if len(cluster.Spec.HeadGroupSpec.IngressOptions.Ingresses) == 0 {
		routes, err := BuildRouteForHeadService(cluster)
		if err != nil {
			return nil, err
		}

		serviceName, err := utils.GenerateHeadServiceName("RayService", cluster.Spec, cluster.Name)
		if err != nil {
			return nil, err
		}
		routes[0].ObjectMeta.Name = serviceName
		routes[0].ObjectMeta.Namespace = service.Namespace
		routes[0].ObjectMeta.Labels[RayServiceLabelKey] = service.Name
		routes[0].ObjectMeta.Labels[RayIDLabelKey] = utils.CheckLabel(utils.GenerateIdentifier(service.Name, rayv1.HeadNode))

		return routes[0], nil
	}
	return nil, nil
}
