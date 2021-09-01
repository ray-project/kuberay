package common

import (
	"fmt"
	rayiov1alpha1 "ray-operator/api/v1alpha1"
	"ray-operator/controllers/utils"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// DefaultServiceSelector creates a service in case the service is missing from the CR RayCluster
func DefaultServiceSelector(instance rayiov1alpha1.RayCluster) map[string]string {
	return map[string]string{
		"identifier": utils.TrimName(fmt.Sprintf("%s-%s", instance.Name, rayiov1alpha1.HeadNode)),
	}
}

// BuildServiceForHeadPod Builds the service for a pod. Currently, there is only one service that allows
// the worker nodes to connect to the head node.
func BuildServiceForHeadPod(instance rayiov1alpha1.RayCluster) *corev1.Service {
	if instance.Spec.HeadService.Namespace == "" {
		if instance.Namespace != "" {
			// the Custom resource namespace is assumed to be the same for all the pods and the head service.
			instance.Spec.HeadService.Namespace = instance.Namespace
		} else {
			instance.Spec.HeadService.Namespace = "default"
		}
	}
	if instance.Spec.HeadService.Spec.Selector == nil {
		instance.Spec.HeadService.Spec.Selector = DefaultServiceSelector(instance)
	} else {
		if _, ok := instance.Spec.HeadService.Spec.Selector["identifier"]; !ok {
			instance.Spec.HeadService.Spec.Selector["identifier"] = utils.TrimName(fmt.Sprintf("%s-%s", instance.Name, rayiov1alpha1.HeadNode))
		}
	}
	// Making sure any existing selector is valid in length
	instance.Spec.HeadService.Spec.Selector = utils.TrimMap(instance.Spec.HeadService.Spec.Selector)
	if instance.Spec.HeadService.Spec.Ports == nil {
		instance.Spec.HeadService.Spec.Ports = []corev1.ServicePort{{Name: "redis", Port: int32(defaultRedisPort)}}
	}
	instance.Spec.HeadService.Spec.ClusterIP = corev1.ClusterIPNone //headless service
	rayPodSvc := &instance.Spec.HeadService
	rayPodSvc.Name = checkSvcName(instance)
	return rayPodSvc
}

// checkServiceName verfies that we prefix the Ray cluster name to the service name
// this avoid having service conflicts in case two  Ray clusters define the same service name
func checkSvcName(instance rayiov1alpha1.RayCluster) (name string) {
	if !strings.HasPrefix(instance.Spec.HeadService.Name, fmt.Sprintf("%s-%s", "svc", instance.Name)) {
		amendedName := fmt.Sprintf("%s-%s-%s", "svc", instance.Name, instance.Spec.HeadService.Name)
		amendedName = utils.TrimName(amendedName)
		errorList, nameIsValid := utils.IsValid(amendedName)
		if !nameIsValid {
			log.Error(fmt.Errorf("validate pod name error"), strings.Join(errorList, " "))
		}
		log.Info("checkSvcName ", "svc name amended", amendedName)
		return amendedName
	}
	return utils.TrimName(fmt.Sprintf("%s-%s", "svc", instance.Spec.HeadService.Name))
}
