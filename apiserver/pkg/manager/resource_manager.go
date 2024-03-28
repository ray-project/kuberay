package manager

import (
	"context"

	api "github.com/ray-project/kuberay/proto/go_client"
	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	corev1 "k8s.io/api/core/v1"
)

// ResourceManager can be used by services to operate resources
// kubernetes objects and potential db objects underneath operations should be encapsulated at this layer
type ResourceManager interface {
	CreateCluster(ctx context.Context, apiCluster *api.Cluster) (*rayv1api.RayCluster, error)
	GetCluster(ctx context.Context, clusterName string, namespace string) (*rayv1api.RayCluster, error)
	ListClusters(ctx context.Context, namespace string) ([]*rayv1api.RayCluster, error)
	ListAllClusters(ctx context.Context) ([]*rayv1api.RayCluster, error)
	DeleteCluster(ctx context.Context, clusterName string, namespace string) error
	CreateComputeTemplate(ctx context.Context, runtime *api.ComputeTemplate) (*corev1.ConfigMap, error)
	GetComputeTemplate(ctx context.Context, name string, namespace string) (*corev1.ConfigMap, error)
	ListComputeTemplates(ctx context.Context, namespace string) ([]*corev1.ConfigMap, error)
	ListAllComputeTemplates(ct context.Context) ([]*corev1.ConfigMap, error)
	DeleteComputeTemplate(ctx context.Context, name string, namespace string) error
	CreateJob(ctx context.Context, apiJob *api.RayJob) (*rayv1api.RayJob, error)
	GetJob(ctx context.Context, jobName string, namespace string) (*rayv1api.RayJob, error)
	ListJobs(ctx context.Context, namespace string) ([]*rayv1api.RayJob, error)
	ListAllJobs(ctx context.Context) ([]*rayv1api.RayJob, error)
	DeleteJob(ctx context.Context, jobName string, namespace string) error
	CreateService(ctx context.Context, apiService *api.RayService) (*rayv1api.RayService, error)
	UpdateRayService(ctx context.Context, apiService *api.RayService) (*rayv1api.RayService, error)
	GetService(ctx context.Context, serviceName, namespace string) (*rayv1api.RayService, error)
	ListServices(ctx context.Context, namespace string) ([]*rayv1api.RayService, error)
	ListAllServices(ctx context.Context) ([]*rayv1api.RayService, error)
	DeleteService(ctx context.Context, serviceName, namespace string) error
	GetClusterEvents(ctx context.Context, clusterName string, namespace string) ([]corev1.Event, error)
	GetServiceEvents(ctx context.Context, service rayv1api.RayService) ([]corev1.Event, error)
}
