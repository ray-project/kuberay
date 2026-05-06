package managercache

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// K8sControllerRuntimeCacheSelectors returns a map[client.Object]cache.ByObject that scopes the manager's
// informer cache to only watch KubeRay-managed Jobs (filtered by app.kubernetes.io/created-by=kuberay-operator)
// and Ray node Pods (filtered by ray.io/node-type in head|worker|redis-cleanup).
func K8sControllerRuntimeCacheSelectors() (map[client.Object]cache.ByObject, error) {
	createByLabel, err := labels.NewRequirement(utils.KubernetesCreatedByLabelKey, selection.Equals, []string{utils.ComponentName})
	if err != nil {
		return nil, err
	}
	rayNodeTypeLabel, err := labels.NewRequirement(
		utils.RayNodeTypeLabelKey,
		selection.In,
		[]string{string(rayv1.HeadNode), string(rayv1.WorkerNode), string(rayv1.RedisCleanupNode)},
	)
	if err != nil {
		return nil, err
	}

	jobSelector := labels.NewSelector().Add(*createByLabel)
	podSelector := labels.NewSelector().Add(*rayNodeTypeLabel)
	return map[client.Object]cache.ByObject{
		&batchv1.Job{}: {Label: jobSelector},
		&corev1.Pod{}:  {Label: podSelector},
	}, nil
}
