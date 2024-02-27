package common

import (
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"k8s.io/apimachinery/pkg/types"
)

func RayClusterServeServiceNamespacedName(instance *rayv1.RayCluster) types.NamespacedName {
	return types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      utils.GenerateServeServiceName(instance.Name),
	}
}

func RayClusterHeadlessServiceNamespacedName(instance *rayv1.RayCluster) types.NamespacedName {
	return types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      utils.GenerateHeadlessServiceName(instance.Name),
	}
}

func RayClusterAutoscalerRoleNamespacedName(instance *rayv1.RayCluster) types.NamespacedName {
	return types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}
}

func RayClusterAutoscalerRoleBindingNamespacedName(instance *rayv1.RayCluster) types.NamespacedName {
	return types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}
}

func RayClusterAutoscalerServiceAccountNamespacedName(instance *rayv1.RayCluster) types.NamespacedName {
	return types.NamespacedName{Namespace: instance.Namespace, Name: utils.GetHeadGroupServiceAccountName(instance)}
}

func RayServiceServeServiceNamespacedName(rayService *rayv1.RayService) types.NamespacedName {
	if rayService.Spec.ServeService != nil && rayService.Spec.ServeService.Name != "" {
		return types.NamespacedName{
			Namespace: rayService.Namespace, // We do not respect s.Spec.ServeService.Namespace intentionally. Ref: https://github.com/ray-project/kuberay/blob/f6b4c3126654d1ef42965abc781846624b8e5dfc/ray-operator/controllers/ray/common/service.go#L298
			Name:      rayService.Spec.ServeService.Name,
		}
	}
	return types.NamespacedName{
		Namespace: rayService.Namespace,
		Name:      utils.GenerateServeServiceName(rayService.Name),
	}
}

func RayServiceActiveRayClusterNamespacedName(rayService *rayv1.RayService) types.NamespacedName {
	return types.NamespacedName{Name: rayService.Status.ActiveServiceStatus.RayClusterName, Namespace: rayService.Namespace}
}

func RayServicePendingRayClusterNamespacedName(rayService *rayv1.RayService) types.NamespacedName {
	return types.NamespacedName{Name: rayService.Status.PendingServiceStatus.RayClusterName, Namespace: rayService.Namespace}
}

// RayJobK8sJobNamespacedName is the only place to associate the RayJob with the submitter Kubernetes Job.
func RayJobK8sJobNamespacedName(rayJob *rayv1.RayJob) types.NamespacedName {
	return types.NamespacedName{
		Namespace: rayJob.Namespace,
		Name:      rayJob.Name,
	}
}

func RayJobRayClusterNamespacedName(rayJob *rayv1.RayJob) types.NamespacedName {
	return types.NamespacedName{
		Name:      rayJob.Status.RayClusterName,
		Namespace: rayJob.Namespace,
	}
}
