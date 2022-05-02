package manager

import (
	"context"

	"github.com/ray-project/kuberay/apiserver/pkg/model"

	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/raycluster/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const DefaultNamespace = "ray-system"

// ResourceManagerInterface can be used by services to operate resources
// kubernetes objects and potential db objects underneath operations should be encapsulated at this layer
type ResourceManagerInterface interface {
	CreateCluster(ctx context.Context, apiCluster *api.Cluster) (*v1alpha1.RayCluster, error)
	GetCluster(ctx context.Context, clusterName string, namespace string) (*v1alpha1.RayCluster, error)
	ListClusters(ctx context.Context) ([]*v1alpha1.RayCluster, error)
	DeleteCluster(ctx context.Context, clusterName string, namespace string) error
	CreateComputeTemplate(ctx context.Context, runtime *api.ComputeTemplate) (*v1.ConfigMap, error)
	GetComputeTemplate(ctx context.Context, name string) (*v1.ConfigMap, error)
	ListComputeTemplates(ctx context.Context) ([]*v1.ConfigMap, error)
	DeleteComputeTemplate(ctx context.Context, name string) error
}

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

func (r *ResourceManager) getKubernetesConfigMapClient(namespace string) clientv1.ConfigMapInterface {
	return r.clientManager.KubernetesClient().ConfigMapClient(namespace)
}

// clusters
func (r *ResourceManager) CreateCluster(ctx context.Context, apiCluster *api.Cluster) (*v1alpha1.RayCluster, error) {
	// populate cluster map
	computeTemplateDict, err := r.populateComputeTemplate(ctx, apiCluster)
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to populate compute template for (%s/%s)", apiCluster.Namespace, apiCluster.Name)
	}

	// convert *api.Cluster to v1alpha1.RayCluster
	rayCluster := util.NewRayCluster(apiCluster, computeTemplateDict)

	// set our own fields.
	clusterAt := r.clientManager.Time().Now().String()
	rayCluster.Annotations["ray.io/creation-timestamp"] = clusterAt

	newRayCluster, err := r.getRayClusterClient(apiCluster.Namespace).Create(ctx, rayCluster.Get(), metav1.CreateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to create a cluster for (%s/%s)", rayCluster.Namespace, rayCluster.Name)
	}

	return newRayCluster, nil
}

func (r *ResourceManager) populateComputeTemplate(ctx context.Context, cluster *api.Cluster) (map[string]*api.ComputeTemplate, error) {
	dict := map[string]*api.ComputeTemplate{}
	// populate head compute template
	name := cluster.ClusterSpec.HeadGroupSpec.ComputeTemplate
	configMap, err := r.GetComputeTemplate(ctx, name)
	if err != nil {
		return nil, err
	}
	computeTemplate := model.FromKubeToAPIComputeTemplate(configMap)
	dict[name] = computeTemplate

	// populate worker compute template
	for _, spec := range cluster.ClusterSpec.WorkerGroupSepc {
		name := spec.ComputeTemplate
		if _, exist := dict[name]; !exist {
			configMap, err := r.GetComputeTemplate(ctx, name)
			if err != nil {
				return nil, err
			}
			computeTemplate := model.FromKubeToAPIComputeTemplate(configMap)
			dict[name] = computeTemplate
		}
	}

	return dict, nil
}

func (r *ResourceManager) GetCluster(ctx context.Context, clusterName string, namespace string) (*v1alpha1.RayCluster, error) {
	if len(clusterName) == 0 {
		return nil, util.NewInvalidInputError("Cluster name is empty, failed to get the cluster.")
	}

	if len(namespace) == 0 {
		return nil, util.NewInvalidInputError("Namespace is empty, failed to get the cluster.")
	}

	client := r.getRayClusterClient(namespace)
	return getClusterByName(ctx, client, clusterName)
}

func (r *ResourceManager) ListClusters(ctx context.Context, namespace string) ([]*v1alpha1.RayCluster, error) {
	if len(namespace) == 0 {
		return nil, util.NewInvalidInputError("Namespace is empty, failed to list clusters.")
	}

	rayClusterList, err := r.getRayClusterClient(namespace).List(ctx, metav1.ListOptions{})
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

func (r *ResourceManager) DeleteCluster(ctx context.Context, clusterName string, namespace string) error {
	if len(clusterName) == 0 {
		return util.NewInvalidInputError("Cluster name is empty, failed to delete the cluster.")
	}

	if len(namespace) == 0 {
		return util.NewInvalidInputError("Namespace is empty, failed to delete the cluster.")
	}

	client := r.getRayClusterClient(namespace)
	cluster, err := getClusterByName(ctx, client, clusterName)
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

// Compute Runtimes
func (r *ResourceManager) CreateComputeTemplate(ctx context.Context, runtime *api.ComputeTemplate) (*v1.ConfigMap, error) {
	_, err := r.GetComputeTemplate(ctx, runtime.Name)
	if err == nil {
		return nil, util.NewAlreadyExistError("Compute template with name %s already exists in namespace %s", runtime.Name, DefaultNamespace)
	}

	computeTemplate, err := util.NewComputeTemplate(runtime, DefaultNamespace)
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to convert compute runtime (%s/%s)", DefaultNamespace, runtime.Name)
	}

	client := r.getKubernetesConfigMapClient(DefaultNamespace)
	newRuntime, err := client.Create(ctx, computeTemplate, metav1.CreateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to create a compute runtime for (%s/%s)", DefaultNamespace, runtime.Name)
	}

	return newRuntime, nil
}

func (r *ResourceManager) GetComputeTemplate(ctx context.Context, name string) (*v1.ConfigMap, error) {
	client := r.getKubernetesConfigMapClient(DefaultNamespace)
	return getComputeTemplateByName(ctx, client, name)
}

func (r *ResourceManager) ListComputeTemplates(ctx context.Context) ([]*v1.ConfigMap, error) {
	client := r.getKubernetesConfigMapClient(DefaultNamespace)
	configMapList, err := client.List(ctx, metav1.ListOptions{LabelSelector: "ray.io/config-type=compute-template"})
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

func (r *ResourceManager) DeleteComputeTemplate(ctx context.Context, name string) error {
	client := r.getKubernetesConfigMapClient(DefaultNamespace)

	configMap, err := getComputeTemplateByName(ctx, client, name)
	if err != nil {
		return util.Wrap(err, "Get compute template failure")
	}

	if err := client.Delete(ctx, configMap.Name, metav1.DeleteOptions{}); err != nil {
		return util.NewInternalServerError(err, "failed to delete compute template %v.", name)
	}

	return nil
}

// getClusterByName returns the Kubernetes RayCluster object by given name and client
func getClusterByName(ctx context.Context, client rayiov1alpha1.RayClusterInterface, name string) (*v1alpha1.RayCluster, error) {
	cluster, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, util.Wrap(err, "Get Cluster failed")
	}

	return cluster, nil
}

// getComputeTemplateByName returns the Kubernetes configmap object by given name and client
func getComputeTemplateByName(ctx context.Context, client clientv1.ConfigMapInterface, name string) (*v1.ConfigMap, error) {
	runtime, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, util.Wrap(err, "Get compute template failed")
	}

	return runtime, nil
}
