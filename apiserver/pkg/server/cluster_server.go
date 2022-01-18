package server

import (
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
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
	panic("Implement me")
}

// Finds a specific Cluster by ID.
func (s *ClusterServer) GetCluster(ctx context.Context, request *api.GetClusterRequest) (*api.Cluster, error) {
	panic("Implement me")
}

// Finds all Clusters. Supports pagination, and sorting on certain fields.
func (s *ClusterServer) ListCluster(ctx context.Context, request *api.ListClustersRequest) (*api.ListClustersResponse, error) {
	panic("Implement me")
}

// Deletes an Cluster without deleting the Cluster's runs and jobs. To
// avoid unexpected behaviors, delete an Cluster's runs and jobs before
// deleting the Cluster.
func (s *ClusterServer) DeleteCluster(ctx context.Context, request *api.DeleteClusterRequest) (*empty.Empty, error) {
	panic("Implement me")
}

func ValidateCreateClusterRequest(request *api.CreateClusterRequest) error {
	if request.Cluster.Name == "" {
		return util.NewInvalidInputError("Cluster name is empty. Please specify a valid value.")
	}

	if request.Cluster.User == "" {
		return util.NewInvalidInputError("User who create the cluster is empty. Please specify a valid value.")
	}

	return nil
}

func NewClusterServer(resourceManager *manager.ResourceManager, options *ClusterServerOptions) *ClusterServer {
	return &ClusterServer{resourceManager: resourceManager, options: options}
}
