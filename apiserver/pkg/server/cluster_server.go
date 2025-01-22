package server

import (
	"context"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/model"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"google.golang.org/protobuf/types/known/emptypb"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
)

type ClusterServerOptions struct {
	CollectMetrics bool
}

// implements `type ClusterServiceServer interface` in cluster_grpc.pb.go
// ClusterServer is the server API for ClusterService service.
type ClusterServer struct {
	resourceManager *manager.ResourceManager
	options         *ClusterServerOptions
	api.UnimplementedClusterServiceServer
}

// Creates a new Cluster.
func (s *ClusterServer) CreateCluster(ctx context.Context, request *api.CreateClusterRequest) (*api.Cluster, error) {
	if err := ValidateCreateClusterRequest(request); err != nil {
		return nil, util.Wrap(err, "Validate create cluster request failed.")
	}

	// use the namespace in the request to override the namespace in the cluster definition
	request.Cluster.Namespace = request.Namespace

	cluster, err := s.resourceManager.CreateCluster(ctx, request.Cluster)
	if err != nil {
		return nil, util.Wrap(err, "Create Cluster failed.")
	}
	events, err := s.resourceManager.GetClusterEvents(ctx, cluster.Name, cluster.Namespace)
	if err != nil {
		klog.Warningf("Failed to get cluster's event, cluster: %s/%s, err: %v", cluster.Namespace, cluster.Name, err)
	}

	return model.FromCrdToApiCluster(cluster, events), nil
}

// Finds a specific Cluster by cluster name.
func (s *ClusterServer) GetCluster(ctx context.Context, request *api.GetClusterRequest) (*api.Cluster, error) {
	if request.Name == "" {
		return nil, util.NewInvalidInputError("Cluster name is empty. Please specify a valid value.")
	}

	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("Namespace is empty. Please specify a valid value.")
	}

	cluster, err := s.resourceManager.GetCluster(ctx, request.Name, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "Get cluster failed.")
	}
	events, err := s.resourceManager.GetClusterEvents(ctx, cluster.Name, cluster.Namespace)
	if err != nil {
		klog.Warningf("Failed to get cluster's event, cluster: %s/%s, err: %v", cluster.Namespace, cluster.Name, err)
	}

	return model.FromCrdToApiCluster(cluster, events), nil
}

// Finds all Clusters in a given namespace.
// TODO: Supports pagination and sorting on certain fields when we have DB support. request needs to be extended.
func (s *ClusterServer) ListCluster(ctx context.Context, request *api.ListClustersRequest) (*api.ListClustersResponse, error) {
	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("Namespace is empty. Please specify a valid value.")
	}

	clusters, err := s.resourceManager.ListClusters(ctx, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "List clusters failed.")
	}
	clusterEventMap := make(map[string][]corev1.Event)
	for _, cluster := range clusters {
		clusterEvents, err := s.resourceManager.GetClusterEvents(ctx, cluster.Name, cluster.Namespace)
		if err != nil {
			klog.Warningf("Failed to get cluster's event, cluster: %s/%s, err: %v", cluster.Namespace, cluster.Name, err)
			continue
		}
		clusterEventMap[cluster.Name] = clusterEvents
	}

	return &api.ListClustersResponse{
		Clusters: model.FromCrdToApiClusters(clusters, clusterEventMap),
	}, nil
}

// Finds all Clusters in all namespaces.
// TODO: Supports pagination and sorting on certain fields when we have DB support. request needs to be extended.
func (s *ClusterServer) ListAllClusters(ctx context.Context, request *api.ListAllClustersRequest) (*api.ListAllClustersResponse, error) {
	clusters, err := s.resourceManager.ListAllClusters(ctx)
	if err != nil {
		return nil, util.Wrap(err, "List clusters from all namespaces failed.")
	}
	clusterEventMap := make(map[string][]corev1.Event)
	for _, cluster := range clusters {
		clusterEvents, err := s.resourceManager.GetClusterEvents(ctx, cluster.Name, cluster.Namespace)
		if err != nil {
			klog.Warningf("Failed to get cluster's event, cluster: %s/%s, err: %v", cluster.Namespace, cluster.Name, err)
			continue
		}
		clusterEventMap[cluster.Name] = clusterEvents
	}

	return &api.ListAllClustersResponse{
		Clusters: model.FromCrdToApiClusters(clusters, clusterEventMap),
	}, nil
}

// Deletes an Cluster without deleting the Cluster's runs and jobs. To
// avoid unexpected behaviors, delete an Cluster's runs and jobs before
// deleting the Cluster.
func (s *ClusterServer) DeleteCluster(ctx context.Context, request *api.DeleteClusterRequest) (*emptypb.Empty, error) {
	if request.Name == "" {
		return nil, util.NewInvalidInputError("Cluster name is empty. Please specify a valid value.")
	}

	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("Namespace is empty. Please specify a valid value.")
	}

	// TODO: do we want to have some logics here to check cluster exist here? or put it inside resourceManager
	if err := s.resourceManager.DeleteCluster(ctx, request.Name, request.Namespace); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func ValidateCreateClusterRequest(request *api.CreateClusterRequest) error {
	if request.Namespace == "" {
		return util.NewInvalidInputError("Namespace is empty. Please specify a valid value.")
	}

	if request.Namespace != request.Cluster.Namespace {
		return util.NewInvalidInputError("The namespace in the request is different from the namespace in the cluster definition.")
	}

	if request.Cluster.Name == "" {
		return util.NewInvalidInputError("Cluster name is empty. Please specify a valid value.")
	}

	if request.Cluster.User == "" {
		return util.NewInvalidInputError("User who create the cluster is empty. Please specify a valid value.")
	}

	if err := ValidateClusterSpec(request.Cluster.ClusterSpec); err != nil {
		return err
	}

	return nil
}

func NewClusterServer(resourceManager *manager.ResourceManager, options *ClusterServerOptions) *ClusterServer {
	return &ClusterServer{resourceManager: resourceManager, options: options}
}
