package common

import (
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildServiceForHeadPod Builds the service for a pod. Currently, there is only one service that allows
// the worker nodes to connect to the head node.
func BuildServiceForHeadPod(cluster rayiov1alpha1.RayCluster) (*corev1.Service, error) {
	labels := map[string]string{
		RayClusterLabelKey:  cluster.Name,
		RayNodeTypeLabelKey: string(rayiov1alpha1.HeadNode),
		RayIDLabelKey:       utils.GenerateIdentifier(cluster.Name, rayiov1alpha1.HeadNode),
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

	ports, _ := getPortsFromCluster(cluster)
	// Assign default ports
	if len(ports) == 0 {
		ports[DefaultClientPortName] = DefaultClientPort
		ports[DefaultRedisPortName] = DefaultRedisPort
		ports[DefaultDashboardName] = DefaultDashboardPort
	}

	for name, port := range ports {
		svcPort := corev1.ServicePort{Name: name, Port: port}
		service.Spec.Ports = append(service.Spec.Ports, svcPort)
	}

	return service, nil
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
