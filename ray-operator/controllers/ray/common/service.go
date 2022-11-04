package common

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// HeadServiceLabels returns the default labels for a cluster's head service.
func HeadServiceLabels(cluster rayiov1alpha1.RayCluster) map[string]string {
	return map[string]string{
		RayClusterLabelKey:                cluster.Name,
		RayNodeTypeLabelKey:               string(rayiov1alpha1.HeadNode),
		RayIDLabelKey:                     utils.CheckLabel(utils.GenerateIdentifier(cluster.Name, rayiov1alpha1.HeadNode)),
		KubernetesApplicationNameLabelKey: ApplicationName,
		KubernetesCreatedByLabelKey:       ComponentName,
	}
}

// BuildServiceForHeadPod Builds the service for a pod. Currently, there is only one service that allows
// the worker nodes to connect to the head node.
func BuildServiceForHeadPod(cluster rayiov1alpha1.RayCluster, labels map[string]string) (*corev1.Service, error) {
	if labels == nil {
		labels = make(map[string]string)
	}

	default_labels := HeadServiceLabels(cluster)

	for k, v := range default_labels {
		if _, ok := labels[k]; !ok {
			labels[k] = v
		}
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateServiceName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    []corev1.ServicePort{},
			Type:     cluster.Spec.HeadGroupSpec.ServiceType,
		},
	}

	ports := getServicePorts(cluster)
	defaultAppProtocol := DefaultServiceAppProtocol
	for name, port := range ports {
		svcPort := corev1.ServicePort{Name: name, Port: port, AppProtocol: &defaultAppProtocol}
		service.Spec.Ports = append(service.Spec.Ports, svcPort)
	}

	return service, nil
}

// BuildHeadServiceForRayService Builds the service for a pod. Currently, there is only one service that allows
// the worker nodes to connect to the head node.
// RayService controller updates the service whenever a new RayCluster serves the traffic.
func BuildHeadServiceForRayService(rayService rayiov1alpha1.RayService, rayCluster rayiov1alpha1.RayCluster) (*corev1.Service, error) {
	service, err := BuildServiceForHeadPod(rayCluster, nil)
	if err != nil {
		return nil, err
	}

	service.ObjectMeta.Name = utils.GenerateServiceName(rayService.Name)
	service.ObjectMeta.Namespace = rayService.Namespace
	service.ObjectMeta.Labels = map[string]string{
		RayServiceLabelKey:  rayService.Name,
		RayNodeTypeLabelKey: string(rayiov1alpha1.HeadNode),
		RayIDLabelKey:       utils.CheckLabel(utils.GenerateIdentifier(rayService.Name, rayiov1alpha1.HeadNode)),
	}

	return service, nil
}

// BuildServeServiceForRayService builds the service for head node and worker nodes who have healthy http proxy to serve traffics.
func BuildServeServiceForRayService(rayService rayiov1alpha1.RayService, rayCluster rayiov1alpha1.RayCluster) (*corev1.Service, error) {
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
func BuildDashboardService(cluster rayiov1alpha1.RayCluster) (*corev1.Service, error) {
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
func getServicePorts(cluster rayiov1alpha1.RayCluster) map[string]int32 {
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

	return ports
}

// getPortsFromCluster get the ports from head container and directly map them in service
// It's user's responsibility to maintain rayStartParam ports and container ports mapping
// TODO: Consider to infer ports from rayStartParams (source of truth) in the future.
func getPortsFromCluster(cluster rayiov1alpha1.RayCluster) (map[string]int32, error) {
	svcPorts := map[string]int32{}

	index := utils.FindRayContainerIndex(cluster.Spec.HeadGroupSpec.Template.Spec)
	cPorts := cluster.Spec.HeadGroupSpec.Template.Spec.Containers[index].Ports
	for _, port := range cPorts {
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
func IsAgentServiceEnabled(instance *rayiov1alpha1.RayCluster) bool {
	enableAgentServiceValue, exist := instance.Annotations[EnableAgentServiceKey]
	if exist && enableAgentServiceValue == EnableAgentServiceTrue {
		return true
	}
	return false
}
