package manager

import (
	"context"
	"fmt"
	api "github.com/ray-project/kuberay/api/go_client"
	"github.com/ray-project/kuberay/backend/pkg/model/converter"
	"github.com/ray-project/kuberay/backend/pkg/util"
	"github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/raycluster/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"
)

const DefaultNamespace = "ray-system"

type ResourceManager struct {
	clientManager ClientManagerInterface
}

// It would be easier to discover methods.
func NewResourceManager(clientManager ClientManagerInterface) *ResourceManager {
	return &ResourceManager{
		clientManager: clientManager,
	}
}

// Clients
func (r *ResourceManager) getRayClusterClient(namespace string) rayiov1alpha1.RayClusterInterface {
	return r.clientManager.ClusterClient().RayClusterClient(namespace)
}

func (r *ResourceManager) getKubernetesPodClient(namespace string) clientv1.PodInterface {
	return r.clientManager.KubernetesClient().PodClient(namespace)
}

func (r *ResourceManager) getKubernetesConfigMapClient(namespace string) clientv1.ConfigMapInterface {
	return r.clientManager.KubernetesClient().ConfigMapClient(namespace)
}

// Clusters
func (r *ResourceManager) CreateCluster(ctx context.Context, apiCluster *api.Cluster) (*v1alpha1.RayCluster, error) {
	uuid, err := r.clientManager.UUID().NewRandom()
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to generate Cluster ID.")
	}
	clusterId := uuid.String()
	clusterAt := r.clientManager.Time().Now().String()

	clusterRuntime, err := r.GetClusterRuntime(ctx, "", apiCluster.ClusterRuntime)
	if err != nil {
		return nil, util.Wrap(err, "Get cluster runtime failed")
	}

	computeRuntime, err := r.GetComputeRuntime(ctx, "", apiCluster.ComputeRuntime)
	if err != nil {
		return nil, util.Wrap(err, "Get compute runtime failed")
	}

	// convert *api.Cluster to v1alpha1.RayCluster
	rayCluster := util.NewRayCluster(apiCluster,
		converter.FromKubeToAPIClusterRuntime(clusterRuntime),
		converter.FromKubeToAPIComputeRuntime(computeRuntime))

	// set our own fields.
	rayCluster.Name = clusterId
	rayCluster.Annotations["ray.io/creation-timestamp"] = clusterAt

	newRayCluster, err := r.getRayClusterClient(DefaultNamespace).Create(ctx, rayCluster.Get(), metav1.CreateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to create a cluster for (%s/%s)", rayCluster.Namespace, rayCluster.Name)
	}

	return newRayCluster, nil
}

func (r *ResourceManager) GetCluster(ctx context.Context, clusterId, clusterName string) (*v1alpha1.RayCluster, error) {
	client := r.getRayClusterClient(DefaultNamespace)
	if len(clusterId) != 0 {
		rayCluster, err := client.Get(ctx, clusterId, metav1.GetOptions{})
		if err != nil {
			return nil, util.Wrap(err, "Get RayCluster failed")
		}
		return rayCluster, err
	}

	return getClusterByName(ctx, clusterName, client)
}

func getClusterByName(ctx context.Context, name string, client rayiov1alpha1.RayClusterInterface) (*v1alpha1.RayCluster, error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			util.RayClusterNameLabelKey: name,
		},
	}
	clusters, err := client.List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, util.Wrap(err, "Get Cluster failed")
	}

	if len(clusters.Items) > 1 {
		return nil, fmt.Errorf("find %d duplicates clusters", len(clusters.Items))
	}

	if len(clusters.Items) == 0 {
		return nil, fmt.Errorf("can not find clusters with name %s", name)
	}

	return &clusters.Items[0], nil
}

func (r *ResourceManager) ListClusters(ctx context.Context) ([]*v1alpha1.RayCluster, error) {
	rayClusterList, err := r.getRayClusterClient(DefaultNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, util.Wrap(err, "List RayCluster failed")
	}

	var result []*v1alpha1.RayCluster
	length := len(rayClusterList.Items)
	for i := 0; i < length; i++ {
		result = append(result, &rayClusterList.Items[i])
	}

	return result, nil
}

func (r *ResourceManager) DeleteCluster(ctx context.Context, clusterId, clusterName string) error {
	client := r.getRayClusterClient(DefaultNamespace)

	if len(clusterId) != 0 {
		if _, err := client.Get(ctx, clusterId, metav1.GetOptions{}); err != nil {
			return util.Wrap(err, "Cannot find the cluster.")
		}
		if err := client.Delete(ctx, clusterId, metav1.DeleteOptions{}); err != nil {
			klog.Warningf("Failed to delete cluster %v.", clusterId)
		}
		return nil
	}

	cluster, err := getClusterByName(ctx, clusterName, client)
	if err != nil {
		return util.Wrap(err, "Get cluster failure")
	}

	// Delete Kubernetes resources
	if err := client.Delete(ctx, cluster.Name, metav1.DeleteOptions{}); err != nil {
		// API won't need to delete the ray cluster CR
		return util.NewInternalServerError(err, "Failed to delete cluster %v.", clusterName)
	}

	return nil
}

// TODO: Change status in storage(like db) to indicate if cluster can be used or not.
func (r *ResourceManager) ArchiveCluster(ctx context.Context, id string) error {
	// This is not implemented yet
	return nil
}

func (r *ResourceManager) UnarchiveCluster(ctx context.Context, id string) error {
	// This is not implemented yet
	return nil
}

// Cluster Runtimes
func (r *ResourceManager) CreateClusterRuntime(ctx context.Context, runtime *api.ClusterRuntime) (*v1.ConfigMap, error) {
	id, err := r.clientManager.UUID().NewRandom()
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to generate id")
	}
	clusterRuntime, err := util.NewClusterRuntime(runtime, id.String(), DefaultNamespace)
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to convert cluster runtime (%s/%s)", runtime.Name, DefaultNamespace)
	}

	client := r.clientManager.KubernetesClient().ConfigMapClient(DefaultNamespace)
	newRuntime, err := client.Create(ctx, clusterRuntime, metav1.CreateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to create a cluster runtime for (%s/%s)", clusterRuntime.Name, DefaultNamespace)
	}

	return newRuntime, nil
}

func (r *ResourceManager) GetClusterRuntime(ctx context.Context, id string, name string) (*v1.ConfigMap, error) {
	client := r.clientManager.KubernetesClient().ConfigMapClient(DefaultNamespace)
	if len(id) != 0 {
		runtime, err := client.Get(ctx, id, metav1.GetOptions{})
		if err != nil {
			return nil, util.Wrap(err, "Get cluster runtime failed")
		}
		return runtime, nil
	}

	return getClusterRuntimeByName(ctx, name, client)
}

func getClusterRuntimeByName(ctx context.Context, name string, client clientv1.ConfigMapInterface) (*v1.ConfigMap, error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"ray.io/config-type":     "cluster-runtime",
			"ray.io/cluster-runtime": name,
		},
	}
	runtimes, err := client.List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, util.Wrap(err, "Get cluster runtime failed")
	}

	if len(runtimes.Items) > 1 {
		return nil, fmt.Errorf("find %d duplicates cluster runtimes", len(runtimes.Items))
	}

	if len(runtimes.Items) == 0 {
		return nil, fmt.Errorf("can not find cluster runtime with name %s", name)
	}

	return &runtimes.Items[0], nil
}

func (r *ResourceManager) ListClusterRuntimes(ctx context.Context) ([]*v1.ConfigMap, error) {
	client := r.clientManager.KubernetesClient().ConfigMapClient(DefaultNamespace)
	configMapList, err := client.List(ctx, metav1.ListOptions{LabelSelector: "ray.io/config-type=cluster-runtime"})
	if err != nil {
		return nil, util.Wrap(err, "List cluster runtimes failed")
	}

	var result []*v1.ConfigMap
	length := len(configMapList.Items)
	for i := 0; i < length; i++ {
		result = append(result, &configMapList.Items[i])
	}

	return result, nil
}

func (r *ResourceManager) DeleteClusterRuntime(ctx context.Context, id string, name string) error {
	client := r.clientManager.KubernetesClient().ConfigMapClient(DefaultNamespace)
	if len(id) != 0 {
		if _, err := client.Get(ctx, id, metav1.GetOptions{}); err != nil {
			return util.Wrap(err, "Can not find cluster runtime")
		}

		if err := client.Delete(ctx, id, metav1.DeleteOptions{}); err != nil {
			klog.Warningf("Failed to delete cluster runtime %v. Error: %v", id, err.Error())
		}

		return nil
	}

	configMap, err := getClusterRuntimeByName(ctx, name, client)
	if err != nil {
		return util.Wrap(err, "Get cluster runtime failure")
	}

	if err := client.Delete(ctx, configMap.Name, metav1.DeleteOptions{}); err != nil {
		return util.NewInternalServerError(err,"Failed to delete cluster runtime %v.", name)
	}

	return nil
}

// Compute Runtimes
func (r *ResourceManager) CreateComputeRuntime(ctx context.Context, runtime *api.ComputeRuntime) (*v1.ConfigMap, error) {
	id, err := r.clientManager.UUID().NewRandom()
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to generate id")
	}
	computeRuntime, err := util.NewComputeRuntime(runtime, id.String(), DefaultNamespace)
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to convert compute runtime (%s/%s)", runtime.Name, DefaultNamespace)
	}

	client := r.clientManager.KubernetesClient().ConfigMapClient(DefaultNamespace)
	newRuntime, err := client.Create(ctx, computeRuntime, metav1.CreateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to create a compute runtime for (%s/%s)", runtime.Name, DefaultNamespace)
	}

	return newRuntime, nil
}

func (r *ResourceManager) GetComputeRuntime(ctx context.Context, id string, name string) (*v1.ConfigMap, error) {
	client := r.clientManager.KubernetesClient().ConfigMapClient(DefaultNamespace)

	if len(id) != 0 {
		runtime, err := client.Get(ctx, id, metav1.GetOptions{})
		if err != nil {
			return nil, util.Wrap(err, "Get compute runtime failed")
		}
		return runtime, nil
	}

	return getComputeRuntimeByName(ctx, name, client)
}

func getComputeRuntimeByName(ctx context.Context, name string, client clientv1.ConfigMapInterface) (*v1.ConfigMap, error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"ray.io/config-type":     "compute-runtime",
			"ray.io/compute-runtime": name,
		},
	}
	runtimes, err := client.List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, util.Wrap(err, "Get compute runtime failed")
	}

	if len(runtimes.Items) > 1 {
		return nil, fmt.Errorf("find %d duplicates compute runtimes", len(runtimes.Items))
	}

	if len(runtimes.Items) == 0 {
		return nil, fmt.Errorf("can not find compue runtime with name %s", name)
	}

	return &runtimes.Items[0], nil
}

func (r *ResourceManager) ListComputeRuntimes(ctx context.Context) ([]*v1.ConfigMap, error) {
	client := r.clientManager.KubernetesClient().ConfigMapClient(DefaultNamespace)
	configMapList, err := client.List(ctx, metav1.ListOptions{LabelSelector: "ray.io/config-type=compute-runtime"})
	if err != nil {
		return nil, util.Wrap(err, "List compute runtimes failed")
	}

	var result []*v1.ConfigMap
	length := len(configMapList.Items)
	for i := 0; i < length; i++ {
		result = append(result, &configMapList.Items[i])
	}

	return result, nil
}

func (r *ResourceManager) DeleteComputeRuntime(ctx context.Context, id string, name string) error {
	client := r.clientManager.KubernetesClient().ConfigMapClient(DefaultNamespace)
	if len(id) != 0 {
		if _, err := client.Get(ctx, id, metav1.GetOptions{}); err != nil {
			return util.Wrap(err, "Can not find compute runtime")
		}

		if err := client.Delete(ctx, id, metav1.DeleteOptions{}); err != nil {
			return util.NewInternalServerError(err,"failed to delete compute runtime %v.", name)
		}
		return nil
	}

	configMap, err := getComputeRuntimeByName(ctx, name, client)
	if err != nil {
		return util.Wrap(err, "Get compute runtime failure")
	}

	if err := client.Delete(ctx, configMap.Name, metav1.DeleteOptions{}); err != nil {
		return util.NewInternalServerError(err, "failed to delete compute runtime %v.", name)
	}

	return nil
}
