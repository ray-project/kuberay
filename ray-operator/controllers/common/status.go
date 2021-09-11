package common

import (
	rayiov1alpha1 "github.com/ray-project/ray-contrib/ray-operator/api/v1alpha1"

	v1 "k8s.io/api/core/v1"
)

func CalculateDesiredReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.Replicas
	}

	return count
}

func CalculateMinReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.MinReplicas
	}

	return count
}

func CalculateMaxReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.MaxReplicas
	}

	return count
}

func CalculateAvailableReplicas(pods v1.PodList) int32 {
	count := int32(0)
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending || pod.Status.Phase == v1.PodRunning {
			count++
		}
	}

	return count
}
