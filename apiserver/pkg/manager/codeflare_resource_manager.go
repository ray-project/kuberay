package manager

import (
	"context"
	"fmt"

	mcadApi "github.com/project-codeflare/multi-cluster-app-dispatcher/pkg/apis/controller/v1beta1"

	"github.com/ray-project/kuberay/apiserver/pkg/client"
	"github.com/ray-project/kuberay/apiserver/pkg/codeflare"
	"github.com/ray-project/kuberay/apiserver/pkg/util"

	api "github.com/ray-project/kuberay/proto/go_client"
	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// CodeFlareResourceManager allows for managing clusters and jobs via the Codeflare AppWrapper CRDS.
// For other methods it defers to the default implementation
type CodeFlareResourceManager struct {
	DefaultResourceManager
	mcadClient client.MCADClientInterface
	scheme     *runtime.Scheme
	converter  codeflare.AppWrapperConverter
}

// NewDefaultResourceManager provides a default implementation of the ResourceManager interface.
func NewCodeFlareResourceManager(clientManager ClientManagerInterface, mcadClient client.MCADClientInterface) (*CodeFlareResourceManager, error) {
	scheme := runtime.NewScheme()
	err := rayv1api.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("error encountered while adding RayAPI to the scheme: %w", err)
	}

	err = mcadApi.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("error encountered while adding MCAD API to the scheme: %w", err)
	}

	awc := codeflare.NewAppWrapperConverter(scheme)
	return &CodeFlareResourceManager{
		DefaultResourceManager: *NewDefaultResourceManager(clientManager),
		mcadClient:             mcadClient,
		scheme:                 scheme,
		converter:              awc,
	}, nil
}

func (crm *CodeFlareResourceManager) CreateCluster(ctx context.Context, apiCluster *api.Cluster) (*rayv1api.RayCluster, error) {
	namespace := apiCluster.Namespace
	rayCluster, err := crm.DefaultResourceManager.createRayCluster(ctx, apiCluster, map[string]string{
		"ray.io/codeflare-appwrapper-name": apiCluster.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create ray cluster CRD from api: %w", err)
	}
	clusterAppWrapper, err := crm.converter.AppWrapperFromRayCluster(rayCluster)
	if err != nil {
		return nil, fmt.Errorf("error creating appwrapper for ray cluster '%s/%s': %w", namespace, apiCluster.Name, err)
	}
	_, err = crm.mcadClient.MCADClient(namespace).Create(ctx, clusterAppWrapper, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster app wrapper: %w", err)
	}
	return rayCluster, nil
}

func (crm *CodeFlareResourceManager) GetClusterEvents(ctx context.Context, clusterName string, namespace string) ([]v1.Event, error) {
	clusterAppWrapper, err := crm.getAppWrapper(ctx, namespace, clusterName)
	if err != nil {
		return nil, util.Wrap(err, "get cluster events failed")
	}
	// The appwrapper is in a running state, therefore there should be a running cluster
	// and is safe to get the state of the cluster from the kuberay api object
	if clusterAppWrapper.Status.QueueJobState == mcadApi.AppWrapperCondDispatched &&
		clusterAppWrapper.Status.State == mcadApi.AppWrapperStateActive {
		return crm.DefaultResourceManager.GetClusterEvents(ctx, clusterName, namespace)
	}
	return nil, nil
}

func (crm *CodeFlareResourceManager) GetCluster(ctx context.Context, clusterName string, namespace string) (*rayv1api.RayCluster, error) {
	clusterAppWrapper, err := crm.getAppWrapper(ctx, namespace, clusterName)
	if err != nil {
		return nil, util.Wrap(err, "get cluster failed")
	}
	// The appwrapper is in a running state, therefore there should be a running cluster
	// and is safe to get the state of the cluster from the kuberay api object
	if clusterAppWrapper.Status.QueueJobState == mcadApi.AppWrapperCondDispatched &&
		clusterAppWrapper.Status.State == mcadApi.AppWrapperStateActive {
		return crm.DefaultResourceManager.GetCluster(ctx, clusterName, namespace)
	}
	return crm.converter.RayClusterFromAppWrapper(clusterAppWrapper)
}

func (crm *CodeFlareResourceManager) ListClusters(ctx context.Context, namespace string) ([]*rayv1api.RayCluster, error) {
	rayJobAppWrapperList, err := crm.getAppWrappers(ctx, namespace)
	if err != nil {
		return nil, util.Wrap(err, fmt.Sprintf("List jobs failed in %s", namespace))
	}
	var result []*rayv1api.RayCluster
	for _, jobAppWrapper := range rayJobAppWrapperList {
		rayCluster, err := crm.converter.RayClusterFromAppWrapper(&jobAppWrapper)
		if err != nil {
			return nil, util.Wrap(err, fmt.Sprintf("Failed to extract ray cluster from app wrapper '%s/%s'", namespace, jobAppWrapper.Name))
		}
		result = append(result, rayCluster)
	}

	return result, nil
}

func (crm *CodeFlareResourceManager) ListAllClusters(ctx context.Context) ([]*rayv1api.RayCluster, error) {
	namespaces, err := crm.getKubernetesNamespaceClient().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, util.Wrap(err, "Failed to fetch all Kubernetes namespaces")
	}

	var result []*rayv1api.RayCluster
	for _, namespace := range namespaces.Items {
		rayClusters, err := crm.ListClusters(ctx, namespace.Name)
		if err != nil {
			return nil, util.Wrap(err, fmt.Sprintf("List all clusters failed in %s", namespace.Name))
		}
		result = append(result, rayClusters...)
	}
	return result, nil
}

func (crm *CodeFlareResourceManager) DeleteCluster(ctx context.Context, clusterName string, namespace string) error {
	_, err := crm.getAppWrapper(ctx, namespace, clusterName)
	if err != nil {
		return util.Wrap(err, "delete cluster failed")
	}
	return crm.mcadClient.MCADClient(namespace).Delete(ctx, clusterName, metav1.DeleteOptions{})
}

func (crm *CodeFlareResourceManager) CreateJob(ctx context.Context, apiJob *api.RayJob) (*rayv1api.RayJob, error) {
	namespace := apiJob.Namespace
	rayJob, err := crm.DefaultResourceManager.createRayJob(ctx, apiJob, map[string]string{
		"ray.io/codeflare-appwrapper-name": apiJob.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("can't create a rayJob CRD from apiJob: %w", err)
	}
	jobAppWrapper, err := crm.converter.AppWrapperFromRayJob(rayJob.Get())
	if err != nil {
		return nil, fmt.Errorf("error creating appwrapper for ray job '%s/%s': %w", namespace, apiJob.Name, err)
	}
	_, err = crm.mcadClient.MCADClient(namespace).Create(ctx, jobAppWrapper, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster app wrapper: %w", err)
	}
	return rayJob.Get(), nil
}

func (crm *CodeFlareResourceManager) GetJob(ctx context.Context, jobName string, namespace string) (*rayv1api.RayJob, error) {
	clusterAppWrapper, err := crm.getAppWrapper(ctx, namespace, jobName)
	if err != nil {
		return nil, util.Wrap(err, "get job failed")
	}
	// The appwrapper is in a running state, therefore there should be a running cluster
	// and is safe to get the state of the cluster from the kuberay api object
	if clusterAppWrapper.Status.QueueJobState == mcadApi.AppWrapperCondDispatched &&
		clusterAppWrapper.Status.State == mcadApi.AppWrapperStateActive {
		return crm.DefaultResourceManager.GetJob(ctx, jobName, namespace)
	}
	return crm.converter.RayJobFromAppWrapper(clusterAppWrapper)
}

func (crm *CodeFlareResourceManager) ListJobs(ctx context.Context, namespace string) ([]*rayv1api.RayJob, error) {
	rayJobAppWrapperList, err := crm.getAppWrappers(ctx, namespace)
	if err != nil {
		return nil, util.Wrap(err, fmt.Sprintf("List jobs failed in %s", namespace))
	}
	var result []*rayv1api.RayJob
	for _, jobAppWrapper := range rayJobAppWrapperList {
		rayJob, err := crm.converter.RayJobFromAppWrapper(&jobAppWrapper)
		if err != nil {
			return nil, util.Wrap(err, fmt.Sprintf("Failed to extract ray job from app wrapper '%s/%s'", namespace, jobAppWrapper.Name))
		}
		result = append(result, rayJob)
	}

	return result, nil
}

func (crm *CodeFlareResourceManager) ListAllJobs(ctx context.Context) ([]*rayv1api.RayJob, error) {
	namespaces, err := crm.getKubernetesNamespaceClient().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, util.Wrap(err, "Failed to fetch all Kubernetes namespaces")
	}

	var result []*rayv1api.RayJob
	for _, namespace := range namespaces.Items {
		rayJobs, err := crm.ListJobs(ctx, namespace.Name)
		if err != nil {
			return nil, util.Wrap(err, fmt.Sprintf("List Jobs failed in %s", namespace.Name))
		}
		result = append(result, rayJobs...)
	}
	return result, nil
}

func (crm *CodeFlareResourceManager) DeleteJob(ctx context.Context, jobName string, namespace string) error {
	_, err := crm.getAppWrapper(ctx, namespace, jobName)
	if err != nil {
		return util.Wrap(err, "delete cluster failed")
	}
	return crm.mcadClient.MCADClient(namespace).Delete(ctx, jobName, metav1.DeleteOptions{})
}

func (crm *CodeFlareResourceManager) getAppWrapper(ctx context.Context, namespace, name string) (*mcadApi.AppWrapper, error) {
	appWrapper, err := crm.mcadClient.MCADClient(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, util.NewNotFoundError(err, "app wrapper '%s/%s' not found", namespace, name)
		}

		return nil, util.Wrap(err, "get app wrapper failed")
	}
	if managedBy, ok := appWrapper.Labels[util.KubernetesManagedByLabelKey]; !ok || managedBy != util.ComponentName {
		return nil, fmt.Errorf("app wrapper '%s/%s not managed by %s", namespace, name, util.ComponentName)
	}
	return appWrapper, nil
}

func (crm *CodeFlareResourceManager) getAppWrappers(ctx context.Context, namespace string) ([]mcadApi.AppWrapper, error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			util.KubernetesManagedByLabelKey: util.ComponentName,
		},
	}
	appWrapperList, err := crm.mcadClient.MCADClient(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, util.Wrap(err, fmt.Sprintf("List Jobs failed in %s", namespace))
	}
	return appWrapperList.Items, nil
}
