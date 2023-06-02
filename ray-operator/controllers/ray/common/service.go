package common

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// HeadServiceLabels returns the default labels for a cluster's head service.
func HeadServiceLabels(cluster rayv1alpha1.RayCluster) map[string]string {
	return map[string]string{
		RayClusterLabelKey:                cluster.Name,
		RayNodeTypeLabelKey:               string(rayv1alpha1.HeadNode),
		RayIDLabelKey:                     utils.CheckLabel(utils.GenerateIdentifier(cluster.Name, rayv1alpha1.HeadNode)),
		KubernetesApplicationNameLabelKey: ApplicationName,
		KubernetesCreatedByLabelKey:       ComponentName,
	}
}

// BuildServiceForHeadPod Builds the service for a pod. Currently, there is only one service that allows
// the worker nodes to connect to the head node.
func BuildServiceForHeadPod(cluster rayv1alpha1.RayCluster, labels map[string]string, annotations map[string]string) (*corev1.Service, error) {
	if labels == nil {
		labels = make(map[string]string)
	}

	default_labels := HeadServiceLabels(cluster)

	// selector consists of *only* the keys in default_labels, updated with the values in labels if they exist
	selector := make(map[string]string)
	for k := range default_labels {
		if _, ok := labels[k]; ok {
			selector[k] = labels[k]
		} else {
			selector[k] = default_labels[k]
		}
	}

	// Deep copy the selector to avoid modifying the original object
	labels_for_service := make(map[string]string)
	for k, v := range selector {
		labels_for_service[k] = v
	}

	if annotations == nil {
		annotations = make(map[string]string)
	}

	default_name := utils.GenerateServiceName(cluster.Name)
	default_namespace := cluster.Namespace
	default_type := cluster.Spec.HeadGroupSpec.ServiceType

	defaultAppProtocol := DefaultServiceAppProtocol
	// `ports_int` is a map of port names to port numbers, while `ports` is a list of ServicePort objects
	ports_int := getServicePorts(cluster)
	ports := []corev1.ServicePort{}
	for name, port := range ports_int {
		svcPort := corev1.ServicePort{Name: name, Port: port, AppProtocol: &defaultAppProtocol}
		ports = append(ports, svcPort)
	}
	if cluster.Spec.HeadGroupSpec.HeadService != nil {
		// Use the provided "custom" HeadService.
		// Deep copy the HeadService to avoid modifying the original object
		headService := cluster.Spec.HeadGroupSpec.HeadService.DeepCopy()

		// For the Labels field, merge labels_for_service with custom HeadService labels.
		// If there are overlaps, ignore the custom HeadService labels.
		if headService.ObjectMeta.Labels == nil {
			headService.ObjectMeta.Labels = make(map[string]string)
		}
		for k, v := range labels_for_service {
			headService.ObjectMeta.Labels[k] = v
		}

		// For the selector, ignore any custom HeadService selectors or labels.
		headService.Spec.Selector = selector

		// Merge annotations with custom HeadService annotations. If there are overlaps,
		// ignore the custom HeadService annotations.
		if headService.ObjectMeta.Annotations == nil {
			headService.ObjectMeta.Annotations = make(map[string]string)
		}
		for k, v := range annotations {
			// if the key is present, log a warning message
			if _, ok := headService.ObjectMeta.Annotations[k]; ok {
				log.Info("Overwriting annotation provided in HeadGroupSpec.HeadService with value from HeadServiceAnnotations",
					"annotation_key", k,
					"headService_name", headService.ObjectMeta.Name,
					"annotation_value", v)
			}
			headService.ObjectMeta.Annotations[k] = v
		}

		// Append default ports.
		headService.Spec.Ports = append(headService.Spec.Ports, ports...)

		// If the user has not specified a name, generate one
		if headService.ObjectMeta.Name == "" {
			headService.ObjectMeta.Name = default_name
			log.Info("Using default name for head service.", "default_name", default_name)
		} else {
			log.Info("Overriding default name for head service with provided name in HeadGroupSpec.HeadService",
				"default_name", default_name,
				"provided_name", headService.ObjectMeta.Name)
		}
		// If the user has specified a namespace, ignore it and raise a warning
		if headService.ObjectMeta.Namespace != "" && headService.ObjectMeta.Namespace != default_namespace {
			log.Info("Ignoring namespace provided in HeadGroupSpec.HeadService",
				"provided_namespace", headService.ObjectMeta.Namespace,
				"headService_name", headService.ObjectMeta.Name,
				"default_namespace", default_namespace)
		}
		headService.ObjectMeta.Namespace = default_namespace

		// If the user has not specified a service type, use the cluster's service type
		if headService.Spec.Type == "" {
			headService.Spec.Type = default_type
			log.Info("Using HeadGroupSpec.ServiceType for head service",
				"HeadGroupSpec.ServiceType", default_type,
				"headService.ObjectMeta.Name", headService.ObjectMeta.Name)
		} else {
			log.Info("Overriding HeadGroupSpec.ServiceType for head service with provided type in HeadGroupSpec.HeadService.Spec.Type",
				"HeadGroupSpec.ServiceType", default_type,
				"headService.ObjectMeta.Name", headService.ObjectMeta.Name,
				"HeadGroupSpec.ServiceType", default_type,
				"HeadGroupSpec.HeadService.Spec.Type", headService.Spec.Type)
		}

		return headService, nil
	}

	headService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        default_name,
			Namespace:   default_namespace,
			Labels:      labels_for_service,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    ports,
			Type:     default_type,
		},
	}

	// This change ensures that reconciliation in rayservice_controller will not update the Service spec due to change in ports order
	// sorting the ServicePorts on their name
	if len(headService.Spec.Ports) > 1 {
		sort.SliceStable(headService.Spec.Ports, func(i, j int) bool {
			return headService.Spec.Ports[i].Name < headService.Spec.Ports[j].Name
		})
	}

	return headService, nil
}

// BuildHeadServiceForRayService Builds the service for a pod. Currently, there is only one service that allows
// the worker nodes to connect to the head node.
// RayService controller updates the service whenever a new RayCluster serves the traffic.
func BuildHeadServiceForRayService(rayService rayv1alpha1.RayService, rayCluster rayv1alpha1.RayCluster) (*corev1.Service, error) {
	service, err := BuildServiceForHeadPod(rayCluster, nil, nil)
	if err != nil {
		return nil, err
	}

	service.ObjectMeta.Name = utils.GenerateServiceName(rayService.Name)
	service.ObjectMeta.Namespace = rayService.Namespace
	service.ObjectMeta.Labels = map[string]string{
		RayServiceLabelKey:  rayService.Name,
		RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode),
		RayIDLabelKey:       utils.CheckLabel(utils.GenerateIdentifier(rayService.Name, rayv1alpha1.HeadNode)),
	}

	return service, nil
}

// BuildServeServiceForRayService builds the service for head node and worker nodes who have healthy http proxy to serve traffics.
func BuildServeServiceForRayService(rayService rayv1alpha1.RayService, rayCluster rayv1alpha1.RayCluster) (*corev1.Service, error) {
	labels := map[string]string{
		RayServiceLabelKey:               rayService.Name,
		RayClusterServingServiceLabelKey: utils.GenerateServeServiceLabel(rayService.Name),
	}
	selectorLabels := map[string]string{
		RayClusterLabelKey:               rayCluster.Name,
		RayClusterServingServiceLabelKey: EnableRayClusterServingServiceTrue,
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateServeServiceName(rayService.Name),
			Namespace: rayService.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports:    []corev1.ServicePort{},
			Type:     rayService.Spec.RayClusterSpec.HeadGroupSpec.ServiceType,
		},
	}

	ports := getServicePorts(rayCluster)
	for name, port := range ports {
		if name == DefaultServingPortName {
			svcPort := corev1.ServicePort{Name: name, Port: port}
			service.Spec.Ports = append(service.Spec.Ports, svcPort)
			break
		}
	}

	return service, nil
}

// BuildDashboardService Builds the service for dashboard agent and head node.
func BuildDashboardService(cluster rayv1alpha1.RayCluster) (*corev1.Service, error) {
	labels := map[string]string{
		RayClusterDashboardServiceLabelKey: utils.GenerateDashboardAgentLabel(cluster.Name),
	}
	selectorLabels := map[string]string{
		RayClusterDashboardServiceLabelKey: utils.GenerateDashboardAgentLabel(cluster.Name),
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateDashboardServiceName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports:    []corev1.ServicePort{},
			Type:     cluster.Spec.HeadGroupSpec.ServiceType,
		},
	}

	ports := getServicePorts(cluster)
	for name, port := range ports {
		if name == DefaultDashboardAgentListenPortName {
			svcPort := corev1.ServicePort{Name: name, Port: port}
			service.Spec.Ports = append(service.Spec.Ports, svcPort)
			break
		}
	}

	return service, nil
}

// getServicePorts will either user passing ports or default ports to create service.
func getServicePorts(cluster rayv1alpha1.RayCluster) map[string]int32 {
	ports, err := getPortsFromCluster(cluster)
	// Assign default ports
	if err != nil || len(ports) == 0 {
		ports = getDefaultPorts()
	}

	// Check if agent port is defined. If not, check if enable agent service.
	if _, agentDefined := ports[DefaultDashboardAgentListenPortName]; !agentDefined {
		enableAgentServiceValue, exist := cluster.Annotations[EnableAgentServiceKey]
		if exist && enableAgentServiceValue == EnableAgentServiceTrue {
			// If agent port is not in the config, add default value for it.
			ports[DefaultDashboardAgentListenPortName] = DefaultDashboardAgentListenPort
		}
	}

	// check if metrics port is defined. If not, add default value for it.
	if _, metricsDefined := ports[DefaultMetricsName]; !metricsDefined {
		ports[DefaultMetricsName] = DefaultMetricsPort
	}

	return ports
}

// getPortsFromCluster get the ports from head container and directly map them in service
// It's user's responsibility to maintain rayStartParam ports and container ports mapping
// TODO: Consider to infer ports from rayStartParams (source of truth) in the future.
func getPortsFromCluster(cluster rayv1alpha1.RayCluster) (map[string]int32, error) {
	svcPorts := map[string]int32{}

	index := utils.FindRayContainerIndex(cluster.Spec.HeadGroupSpec.Template.Spec)
	cPorts := cluster.Spec.HeadGroupSpec.Template.Spec.Containers[index].Ports
	for _, port := range cPorts {
		if port.Name == "" {
			port.Name = fmt.Sprint(port.ContainerPort) + "-port"
		}
		svcPorts[port.Name] = port.ContainerPort
	}

	return svcPorts, nil
}

func getDefaultPorts() map[string]int32 {
	return map[string]int32{
		DefaultClientPortName:  DefaultClientPort,
		DefaultRedisPortName:   DefaultRedisPort,
		DefaultDashboardName:   DefaultDashboardPort,
		DefaultMetricsName:     DefaultMetricsPort,
		DefaultServingPortName: DefaultServingPort,
	}
}

// IsAgentServiceEnabled check if the agent service is enabled for RayCluster.
func IsAgentServiceEnabled(instance *rayv1alpha1.RayCluster) bool {
	enableAgentServiceValue, exist := instance.Annotations[EnableAgentServiceKey]
	if exist && enableAgentServiceValue == EnableAgentServiceTrue {
		return true
	}
	return false
}
