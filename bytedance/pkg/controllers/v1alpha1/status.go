package v1alpha1

import (
	"context"
	"time"

	rayiov1alpha1 "github.com/ray-project/ray-contrib/bytedance/pkg/api/v1alpha1"
	"github.com/ray-project/ray-contrib/bytedance/pkg/controllers/common"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// updateStatus updates status of the Ray cluster.
func (r *RayClusterReconciler) updateStatus(cluster *rayiov1alpha1.RayCluster) error {
	pods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: cluster.Name}
	if err := r.List(context.TODO(), &pods, client.InNamespace(cluster.Namespace), filterLabels); err != nil {
		return err
	}

	count := calculateAvailableReplicas(pods)
	if cluster.Status.AvailableReplicas != count {
		cluster.Status.AvailableReplicas = count
	}

	count = calculateDesiredReplicas(cluster)
	if cluster.Status.DesiredReplicas != count {
		cluster.Status.DesiredReplicas = count
	}

	count = calculateMinReplicas(cluster)
	if cluster.Status.DesiredReplicas != count {
		cluster.Status.DesiredReplicas = count
	}

	count = calculateMaxReplicas(cluster)
	if cluster.Status.DesiredReplicas != count {
		cluster.Status.DesiredReplicas = count
	}

	// TODO (@Jeffwan): Fetch head service and check if it's serving
	// We always update cluster no matter if there's one change or not.
	cluster.Status.LastUpdateTime.Time = time.Now()
	if err := r.Status().Update(context.TODO(), cluster); err != nil {
		return err
	}

	return nil
}

func calculateDesiredReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerNodeSpec {
		count += *nodeGroup.Replicas
	}

	return count
}

func calculateMinReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerNodeSpec {
		count += *nodeGroup.MinReplicas
	}

	return count
}

func calculateMaxReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerNodeSpec {
		count += *nodeGroup.MaxReplicas
	}

	return count
}

func calculateAvailableReplicas(pods v1.PodList) int32 {
	count := int32(0)
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodPending || pod.Status.Phase == v1.PodRunning {
			count++
		}
	}

	return count
}
