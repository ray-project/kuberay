package manager

import (
	"context"
	"fmt"

	"github.com/ray-project/kuberay/apiserver/pkg/model"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/ray/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const DefaultNamespace = "ray-system"

// ResourceManagerInterface can be used by services to operate resources
// kubernetes objects and potential db objects underneath operations should be encapsulated at this layer
type ResourceManagerInterface interface {
	CreateCluster(ctx context.Context, apiCluster *api.Cluster) (*v1alpha1.RayCluster, error)
	GetCluster(ctx context.Context, clusterName string, namespace string) (*v1alpha1.RayCluster, error)
	ListClusters(ctx context.Context, namespace string) ([]*v1alpha1.RayCluster, error)
	ListAllClusters(ctx context.Context) ([]*v1alpha1.RayCluster, error)
	DeleteCluster(ctx context.Context, clusterName string, namespace string) error
	CreateComputeTemplate(ctx context.Context, runtime *api.ComputeTemplate) (*v1.ConfigMap, error)
	GetComputeTemplate(ctx context.Context, name string, namespace string) (*v1.ConfigMap, error)
	ListComputeTemplates(ctx context.Context, namespace string) ([]*v1.ConfigMap, error)
	DeleteComputeTemplate(ctx context.Context, name string, namespace string) error
	CreateJob(ctx context.Context, apiJob *api.RayJob) (*v1alpha1.RayJob, error)
	GetJob(ctx context.Context, jobName string, namespace string) (*v1alpha1.RayJob, error)
	ListJobs(ctx context.Context, namespace string) ([]*v1alpha1.RayJob, error)
	ListAllJobs(ctx context.Context) ([]*v1alpha1.RayJob, error)
	DeleteJob(ctx context.Context, jobName string, namespace string) error
	CreateService(ctx context.Context, apiService *api.RayService) (*v1alpha1.RayService, error)
	UpdateRayService(ctx context.Context, request *api.UpdateRayServiceRequest) (*v1alpha1.RayService, error)
	UpdateRayServiceConfigs(ctx context.Context, request *api.UpdateRayServiceConfigsRequest) (*v1alpha1.RayService, error)
	GetService(ctx context.Context, serviceName, namespace string) error
	ListServices(ctx context.Context, namespace string) ([]*v1alpha1.RayService, error)
	ListAllServices(ctx context.Context) ([]*v1alpha1.RayService, error)
	DeleteService(ctx context.Context, serviceName, namespace string) error
	GetClusterEvents(ctx context.Context, clusterName string, namespace string) ([]v1.Event, error)
	GetServiceEvents(ctx context.Context, service v1alpha1.RayService) ([]v1.Event, error)
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

func (r *ResourceManager) getRayJobClient(namespace string) rayiov1alpha1.RayJobInterface {
	return r.clientManager.JobClient().RayJobClient(namespace)
}

func (r *ResourceManager) getRayServiceClient(namespace string) rayiov1alpha1.RayServiceInterface {
	return r.clientManager.ServiceClient().RayServiceClient(namespace)
}

func (r *ResourceManager) getKubernetesConfigMapClient(namespace string) clientv1.ConfigMapInterface {
	return r.clientManager.KubernetesClient().ConfigMapClient(namespace)
}

func (r *ResourceManager) getEventsClient(namespace string) clientv1.EventInterface {
	return r.clientManager.KubernetesClient().EventsClient(namespace)
}

func (r *ResourceManager) getKubernetesNamespaceClient() clientv1.NamespaceInterface {
	return r.clientManager.KubernetesClient().NamespaceClient()
}

// clusters
func (r *ResourceManager) CreateCluster(ctx context.Context, apiCluster *api.Cluster) (*v1alpha1.RayCluster, error) {
	// populate cluster map
	computeTemplateDict, err := r.populateComputeTemplate(ctx, apiCluster.ClusterSpec, apiCluster.Namespace)
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

func (r *ResourceManager) populateComputeTemplate(ctx context.Context, clusterSpec *api.ClusterSpec, nameSpace string) (map[string]*api.ComputeTemplate, error) {
	dict := map[string]*api.ComputeTemplate{}
	// populate head compute template
	name := clusterSpec.HeadGroupSpec.ComputeTemplate
	configMap, err := r.GetComputeTemplate(ctx, name, nameSpace)
	if err != nil {
		return nil, err
	}
	computeTemplate := model.FromKubeToAPIComputeTemplate(configMap)
	dict[name] = computeTemplate

	// populate worker compute template
	for _, spec := range clusterSpec.WorkerGroupSpec {
		name := spec.ComputeTemplate
		if _, exist := dict[name]; !exist {
			configMap, err := r.GetComputeTemplate(ctx, name, nameSpace)
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
	client := r.getRayClusterClient(namespace)
	return getClusterByName(ctx, client, clusterName)
}

func (r *ResourceManager) ListClusters(ctx context.Context, namespace string) ([]*v1alpha1.RayCluster, error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			util.KubernetesManagedByLabelKey: util.ComponentName,
		},
	}
	rayClusterList, err := r.getRayClusterClient(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, util.Wrap(err, fmt.Sprintf("List RayCluster failed in %s", namespace))
	}

	var result []*v1alpha1.RayCluster
	length := len(rayClusterList.Items)
	for i := 0; i < length; i++ {
		result = append(result, &rayClusterList.Items[i])
	}

	return result, nil
}

func (r *ResourceManager) ListAllClusters(ctx context.Context) ([]*v1alpha1.RayCluster, error) {
	namespaces, err := r.getKubernetesNamespaceClient().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, util.Wrap(err, "Failed to fetch all Kubernetes namespaces")
	}

	var result []*v1alpha1.RayCluster
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			util.KubernetesManagedByLabelKey: util.ComponentName,
		},
	}
	for _, namespace := range namespaces.Items {
		rayClusterList, err := r.getRayClusterClient(namespace.Name).List(ctx, metav1.ListOptions{
			LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		})
		if err != nil {
			return nil, util.Wrap(err, fmt.Sprintf("List RayCluster failed in %s", namespace.Name))
		}

		length := len(rayClusterList.Items)
		for i := 0; i < length; i++ {
			result = append(result, &rayClusterList.Items[i])
		}
	}
	return result, nil
}

func (r *ResourceManager) DeleteCluster(ctx context.Context, clusterName string, namespace string) error {
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

func (r *ResourceManager) CreateJob(ctx context.Context, apiJob *api.RayJob) (*v1alpha1.RayJob, error) {
	computeTemplateMap := make(map[string]*api.ComputeTemplate)
	var err error

	// populate cluster map
	if apiJob.ClusterSpec != nil {
		computeTemplateMap, err = r.populateComputeTemplate(ctx, apiJob.ClusterSpec, apiJob.Namespace)
		if err != nil {
			return nil, util.NewInternalServerError(err, "Failed to populate compute template for (%s/%s)", apiJob.Namespace, apiJob.JobId)
		}
	}

	// convert *api.Cluster to v1alpha1.RayCluster
	rayJob := util.NewRayJob(apiJob, computeTemplateMap)

	newRayJob, err := r.getRayJobClient(apiJob.Namespace).Create(ctx, rayJob.Get(), metav1.CreateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to create a job for (%s/%s)", apiJob.Namespace, apiJob.JobId)
	}

	return newRayJob, nil
}

func (r *ResourceManager) GetJob(ctx context.Context, jobName string, namespace string) (*v1alpha1.RayJob, error) {
	client := r.getRayJobClient(namespace)
	return getJobByName(ctx, client, jobName)
}

func (r *ResourceManager) ListJobs(ctx context.Context, namespace string) ([]*v1alpha1.RayJob, error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			util.KubernetesManagedByLabelKey: util.ComponentName,
		},
	}
	rayJobList, err := r.getRayJobClient(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, util.Wrap(err, fmt.Sprintf("List RayCluster failed in %s", namespace))
	}

	var result []*v1alpha1.RayJob
	length := len(rayJobList.Items)
	for i := 0; i < length; i++ {
		result = append(result, &rayJobList.Items[i])
	}

	return result, nil
}

func (r *ResourceManager) ListAllJobs(ctx context.Context) ([]*v1alpha1.RayJob, error) {
	namespaces, err := r.getKubernetesNamespaceClient().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, util.Wrap(err, "Failed to fetch all Kubernetes namespaces")
	}

	var result []*v1alpha1.RayJob
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			util.KubernetesManagedByLabelKey: util.ComponentName,
		},
	}
	for _, namespace := range namespaces.Items {
		rayJobList, err := r.getRayJobClient(namespace.Name).List(ctx, metav1.ListOptions{
			LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		})
		if err != nil {
			return nil, util.Wrap(err, fmt.Sprintf("List RayCluster failed in %s", namespace.Name))
		}

		length := len(rayJobList.Items)
		for i := 0; i < length; i++ {
			result = append(result, &rayJobList.Items[i])
		}
	}
	return result, nil
}

func (r *ResourceManager) DeleteJob(ctx context.Context, jobName string, namespace string) error {
	client := r.getRayJobClient(namespace)
	job, err := getJobByName(ctx, client, jobName)
	if err != nil {
		return util.Wrap(err, "Get job failure")
	}

	// Delete Kubernetes resources
	if err := client.Delete(ctx, job.Name, metav1.DeleteOptions{}); err != nil {
		// API won't need to delete the ray cluster CR
		return util.NewInternalServerError(err, "Failed to delete cluster %v.", jobName)
	}

	return nil
}

func (r *ResourceManager) CreateService(ctx context.Context, apiService *api.RayService) (*v1alpha1.RayService, error) {
	// populate cluster map
	computeTemplateDict, err := r.populateComputeTemplate(ctx, apiService.ClusterSpec, apiService.Namespace)
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to populate compute template for (%s/%s)", apiService.Namespace, apiService.Name)
	}
	rayService := util.NewRayService(apiService, computeTemplateDict)
	createdAt := r.clientManager.Time().Now().String()
	rayService.Annotations["ray.io/creation-timestamp"] = createdAt
	newRayService, err := r.getRayServiceClient(apiService.Namespace).Create(ctx, rayService.Get(), metav1.CreateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to create service for (%s/%s)", rayService.Namespace, rayService.Name)
	}

	return newRayService, nil
}

func (r *ResourceManager) UpdateRayService(ctx context.Context, apiService *api.RayService) (*v1alpha1.RayService, error) {
	name := apiService.Name
	namespace := apiService.Namespace
	client := r.getRayServiceClient(namespace)
	oldService, err := getServiceByName(ctx, client, name)
	if err != nil {
		return nil, util.Wrap(err, fmt.Sprintf("Update service fail, no service named: %s ", name))
	}
	// populate cluster map
	computeTemplateDict, err := r.populateComputeTemplate(ctx, apiService.ClusterSpec, apiService.Namespace)
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to populate compute template for (%s/%s)", apiService.Namespace, apiService.Name)
	}
	rayService := util.NewRayService(apiService, computeTemplateDict)
	rayService.Annotations["ray.io/update-timestamp"] = r.clientManager.Time().Now().String()
	rayService.ResourceVersion = oldService.DeepCopy().ResourceVersion
	newRayService, err := client.Update(ctx, rayService.Get(), metav1.UpdateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to update service for (%s/%s)", rayService.Namespace, rayService.Name)
	}
	return newRayService, nil
}

func (r *ResourceManager) UpdateRayServiceConfigs(ctx context.Context, request *api.UpdateRayServiceConfigsRequest) (*v1alpha1.RayService, error) {
	serviceName := request.Name
	namespace := request.Namespace

	client := r.getRayServiceClient(namespace)
	service, err := getServiceByName(ctx, client, serviceName)
	if err != nil {
		return nil, util.Wrap(err, fmt.Sprintf("Update service fail, no service named: %s ", serviceName))
	}
	updateService := request.GetUpdateService()
	// if workerGroupSpec is not nil, update worker group
	if updateService.WorkerGroupUpdateSpec != nil {
		oldWorkerGroups := service.Spec.RayClusterSpec.WorkerGroupSpecs
		newWorkerGroups := util.UpdateRayServiceWorkerGroupSpecs(updateService.WorkerGroupUpdateSpec, oldWorkerGroups)
		service.Spec.RayClusterSpec.WorkerGroupSpecs = newWorkerGroups
	}
	if updateService.ServeDeploymentGraphSpec != nil {
		oldServeDeploymentGraphSpec := service.Spec.ServeDeploymentGraphSpec
		newServeDeploymentGraphSpec := util.UpdateServeDeploymentGraphSpec(updateService.ServeDeploymentGraphSpec, oldServeDeploymentGraphSpec)
		service.Spec.ServeDeploymentGraphSpec = newServeDeploymentGraphSpec
	}
	service.Annotations["ray.io/update-timestamp"] = r.clientManager.Time().Now().String()
	newService, err := client.Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to update service for (%s/%s)", service.Namespace, service.Name)
	}
	return newService, nil
}

func (r *ResourceManager) GetService(ctx context.Context, serviceName, namespace string) (*v1alpha1.RayService, error) {
	client := r.getRayServiceClient(namespace)
	return getServiceByName(ctx, client, serviceName)
}

func (r *ResourceManager) ListServices(ctx context.Context, namespace string) ([]*v1alpha1.RayService, error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			util.KubernetesManagedByLabelKey: util.ComponentName,
		},
	}
	rayServiceList, err := r.getRayServiceClient(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, util.Wrap(err, fmt.Sprintf("List RayService failed in %s", namespace))
	}
	rayServices := make([]*v1alpha1.RayService, 0)
	for _, service := range rayServiceList.Items {
		rayServices = append(rayServices, &service)
	}

	return rayServices, nil
}

func (r *ResourceManager) ListAllServices(ctx context.Context) ([]*v1alpha1.RayService, error) {
	rayServices := make([]*v1alpha1.RayService, 0)

	namespaces, err := r.getKubernetesNamespaceClient().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, util.Wrap(err, "Failed to fetch all Kubernetes namespaces")
	}

	for _, namespace := range namespaces.Items {
		servicesByNamespace, err := r.ListServices(ctx, namespace.Name)
		if err != nil {
			return nil, util.Wrap(err, "List All Rayservices failed")
		}
		rayServices = append(rayServices, servicesByNamespace...)
	}
	return rayServices, nil
}

func (r *ResourceManager) DeleteService(ctx context.Context, serviceName, namespace string) error {
	client := r.getRayServiceClient(namespace)
	service, err := getServiceByName(ctx, client, serviceName)
	if err != nil {
		return util.Wrap(err, "delete ray service failure")
	}
	if err := client.Delete(ctx, service.Name, metav1.DeleteOptions{}); err != nil {
		return util.NewInternalServerError(err, "failed to delete ray service %s.", service.Name)
	}

	return nil
}

// Compute Runtimes
func (r *ResourceManager) CreateComputeTemplate(ctx context.Context, runtime *api.ComputeTemplate) (*v1.ConfigMap, error) {
	_, err := r.GetComputeTemplate(ctx, runtime.Name, runtime.Namespace)
	if err == nil {
		return nil, util.NewAlreadyExistError("Compute template with name %s already exists in namespace %s", runtime.Name, runtime.Namespace)
	}

	computeTemplate, err := util.NewComputeTemplate(runtime)
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to convert compute runtime (%s/%s)", runtime.Namespace, runtime.Name)
	}

	client := r.getKubernetesConfigMapClient(runtime.Namespace)
	newRuntime, err := client.Create(ctx, computeTemplate, metav1.CreateOptions{})
	if err != nil {
		return nil, util.NewInternalServerError(err, "Failed to create a compute runtime for (%s/%s)", runtime.Namespace, runtime.Name)
	}

	return newRuntime, nil
}

func (r *ResourceManager) GetComputeTemplate(ctx context.Context, name string, namespace string) (*v1.ConfigMap, error) {
	client := r.getKubernetesConfigMapClient(namespace)
	return getComputeTemplateByName(ctx, client, name)
}

func (r *ResourceManager) ListComputeTemplates(ctx context.Context, namespace string) ([]*v1.ConfigMap, error) {
	client := r.getKubernetesConfigMapClient(namespace)
	configMapList, err := client.List(ctx, metav1.ListOptions{LabelSelector: "ray.io/config-type=compute-template"})
	if err != nil {
		return nil, util.Wrap(err, "List compute templates failed")
	}

	var result []*v1.ConfigMap
	length := len(configMapList.Items)
	for i := 0; i < length; i++ {
		result = append(result, &configMapList.Items[i])
	}

	return result, nil
}

func (r *ResourceManager) ListAllComputeTemplates(ctx context.Context) ([]*v1.ConfigMap, error) {
	namespaces, err := r.getKubernetesNamespaceClient().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, util.Wrap(err, "Failed to fetch all Kubernetes namespaces")
	}

	var result []*v1.ConfigMap
	for _, namespace := range namespaces.Items {
		client := r.getKubernetesConfigMapClient(namespace.Name)
		configMapList, err := client.List(ctx, metav1.ListOptions{LabelSelector: "ray.io/config-type=compute-template"})
		if err != nil {
			return nil, util.Wrap(err, fmt.Sprintf("List compute templates failed in %s", namespace.Name))
		}

		length := len(configMapList.Items)
		for i := 0; i < length; i++ {
			result = append(result, &configMapList.Items[i])
		}
	}
	return result, nil
}

func (r *ResourceManager) DeleteComputeTemplate(ctx context.Context, name string, namespace string) error {
	client := r.getKubernetesConfigMapClient(namespace)

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
		if errors.IsNotFound(err) {
			return nil, util.NewNotFoundError(err, "Cluster %s not found", name)
		}

		return nil, util.Wrap(err, "Get Cluster failed")
	}
	if managedBy, ok := cluster.Labels[util.KubernetesManagedByLabelKey]; !ok || managedBy != util.ComponentName {
		return nil, fmt.Errorf("RayCluster with name %s not managed by %s", name, util.ComponentName)
	}

	return cluster, nil
}

// getJobByName returns the Kubernetes RayJob object by given name and client
func getJobByName(ctx context.Context, client rayiov1alpha1.RayJobInterface, name string) (*v1alpha1.RayJob, error) {
	job, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, util.NewNotFoundError(err, "Job %s not found", name)
		}

		return nil, util.Wrap(err, "Get Job failed")
	}
	if managedBy, ok := job.Labels[util.KubernetesManagedByLabelKey]; !ok || managedBy != util.ComponentName {
		return nil, fmt.Errorf("RayCluster with name %s not managed by %s", name, util.ComponentName)
	}

	return job, nil
}

func getServiceByName(ctx context.Context, client rayiov1alpha1.RayServiceInterface, name string) (*v1alpha1.RayService, error) {
	service, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, util.NewNotFoundError(err, "Service %s not found", name)
		}
		return nil, util.Wrap(err, "get service failed")
	}
	if managedBy, ok := service.Labels[util.KubernetesManagedByLabelKey]; !ok || managedBy != util.ComponentName {
		return nil, fmt.Errorf("RayService with name %s not managed by %s", name, util.ComponentName)
	}
	return service, nil
}

// getComputeTemplateByName returns the Kubernetes configmap object by given name and client
func getComputeTemplateByName(ctx context.Context, client clientv1.ConfigMapInterface, name string) (*v1.ConfigMap, error) {
	runtime, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, util.NewNotFoundError(err, "Compute template %s not found", name)
		}

		return nil, util.Wrap(err, "Get compute template failed")
	}

	return runtime, nil
}

func (r *ResourceManager) GetClusterEvents(ctx context.Context, clusterName string, namespace string) ([]v1.Event, error) {
	client := r.getEventsClient(namespace)
	clusterClient := r.getRayClusterClient(namespace)
	return getRayClusterEventsByName(ctx, clusterName, client, clusterClient)
}

func getRayClusterEventsByName(ctx context.Context, name string, client clientv1.EventInterface, clusterClient rayiov1alpha1.RayClusterInterface) ([]v1.Event, error) {
	rayCluster, err := getClusterByName(ctx, clusterClient, name)
	if err != nil {
		return nil, util.Wrap(err, "get raycluster event failed")
	}
	fieldSelectorById := fmt.Sprintf("involvedObject.name=%s", rayCluster.Name)
	events, err := client.List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelectorById,
		TypeMeta:      metav1.TypeMeta{Kind: "RayCluster"},
	})
	if err != nil {
		return nil, util.Wrap(err, "Get Ray Cluster Events failed")
	}
	if len(events.Items) == 0 {
		return nil, fmt.Errorf("No Event with RayCluster name %s", name)
	}

	return events.Items, nil
}

func (r *ResourceManager) GetServiceEvents(ctx context.Context, service v1alpha1.RayService) ([]v1.Event, error) {
	eventClient := r.getEventsClient(service.Namespace)
	events, err := getRayServiceEventsByName(ctx, service.Name, eventClient)
	if err != nil {
		return nil, err
	}
	if len(service.Status.ActiveServiceStatus.RayClusterName) > 0 {
		clusterEvents, err := r.GetClusterEvents(ctx, service.Status.ActiveServiceStatus.RayClusterName, service.Namespace)
		if err != nil {
			clusterEvents = make([]v1.Event, 0)
		}
		events = append(events, clusterEvents...)
	}
	return events, nil
}

func getRayServiceEventsByName(ctx context.Context, name string, client clientv1.EventInterface) ([]v1.Event, error) {
	fieldSelectorById := fmt.Sprintf("involvedObject.name=%s", name)
	events, err := client.List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelectorById,
		TypeMeta:      metav1.TypeMeta{Kind: "RayService"},
	})
	if err != nil {
		return nil, util.Wrap(err, "Get Ray Cluster Events failed")
	}
	if len(events.Items) == 0 {
		return make([]v1.Event, 0), nil
	}

	return events.Items, nil
}
