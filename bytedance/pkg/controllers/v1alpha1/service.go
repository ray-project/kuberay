package v1alpha1

import (
	"context"
	"fmt"

	"github.com/ray-project/ray-contrib/bytedance/pkg/controllers/common"
	"github.com/ray-project/ray-contrib/bytedance/pkg/controllers/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rayiov1alpha1 "github.com/ray-project/ray-contrib/bytedance/pkg/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// createHeadService create a service object
func (r *RayClusterReconciler) createHeadService(rayPodSvc *corev1.Service, cluster *rayiov1alpha1.RayCluster) error {
	if err := controllerutil.SetControllerReference(cluster, rayPodSvc, r.Scheme); err != nil {
		r.Log.Error(err, "Failed to set controller reference for raycluster head service")
	}

	if err := r.Create(context.TODO(), rayPodSvc); err != nil {
		if errors.IsAlreadyExists(err) {
			r.Log.Info(fmt.Sprintf("Pod service %s/%s already exist,no need to create", rayPodSvc.Namespace, rayPodSvc.Name))
			return nil
		}
		r.Log.Error(err, "createHeadService", "Pod Service create error!", "Pod.Service.Error", err)
		return err
	}
	r.Log.Info("createHeadService", "Pod Service created successfully", rayPodSvc.Name)
	r.Recorder.Eventf(cluster, corev1.EventTypeNormal, "Created", "Created service %s", rayPodSvc.Name)
	return nil
}

// buildHeadService Builds the service for a pod. Currently, there is only one service that allows
// the worker nodes to connect to the head node.
func (r *RayClusterReconciler) buildHeadService(cluster rayiov1alpha1.RayCluster) *corev1.Service {
	labels := map[string]string{
		common.RayClusterLabelKey:  cluster.Name,
		common.RayNodeTypeLabelKey: string(rayiov1alpha1.HeadNode),
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
			// In our case, we highly rely on nodePort in test env and ingress in production environment.
			Type: corev1.ServiceTypeNodePort,
		},
	}

	// TODO: check default ones, like redis. We should provide minimum setting.
	ports, _ := getPortsFromCluster(cluster)
	for name, port := range ports {
		svcPort := corev1.ServicePort{Name: name, Port: port}
		service.Spec.Ports = append(service.Spec.Ports, svcPort)
	}

	// Set controller reference
	if err := controllerutil.SetControllerReference(&cluster, service, r.Scheme); err != nil {
		return nil
	}

	return service
}

// getPortsFromCluster get the ports from head container and directly map them in service
func getPortsFromCluster(cluster rayiov1alpha1.RayCluster) (map[string]int32, error) {
	svcPorts := map[string]int32{}

	// TODO: find target container through name instead of using index 0.
	index := utils.FindRayContainerIndex(cluster.Spec.HeadNodeSpec.Template.Spec)
	cPorts := cluster.Spec.HeadNodeSpec.Template.Spec.Containers[index].Ports
	for _, port := range cPorts {
		svcPorts[port.Name] = port.ContainerPort
	}

	return svcPorts, nil
}
