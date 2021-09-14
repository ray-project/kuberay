package server

import (
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	api "github.com/ray-project/kuberay/api/go_client"
	"github.com/ray-project/kuberay/backend/pkg/manager"
	"github.com/ray-project/kuberay/backend/pkg/model/converter"
	"github.com/ray-project/kuberay/backend/pkg/util"
	"google.golang.org/protobuf/types/known/emptypb"
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
	// Is there a way to generate validation from proto like kubebuilder flags?
	if err := ValidateCreateClusterRequest(request); err != nil {
		return nil, util.Wrap(err, "Validate cluster request failed.")
	}

	cluster, err := s.resourceManager.CreateCluster(ctx, request.Cluster)
	if err != nil {
		return nil, util.Wrap(err, "Create Cluster failed.")
	}

	return converter.FromPbToApiCluster(cluster), nil
}

// Finds a specific Cluster by ID.
func (s *ClusterServer) GetCluster(ctx context.Context, request *api.GetClusterRequest) (*api.Cluster, error) {
	cluster, err := s.resourceManager.GetCluster(ctx, request.Id, request.Name)
	if err != nil {
		return nil, util.Wrap(err, "Get cluster failed.")
	}
	return converter.FromPbToApiCluster(cluster), nil
}

// Finds all Clusters. Supports pagination, and sorting on certain fields.
func (s *ClusterServer) ListCluster(ctx context.Context, request *api.ListClustersRequest) (*api.ListClustersResponse, error) {
	clusters, err := s.resourceManager.ListClusters(ctx)
	if err != nil {
		return nil, util.Wrap(err, "List clusters failed.")
	}

	return &api.ListClustersResponse{
		Clusters: converter.FromPbToApiClusters(clusters),
	}, nil
}

// Deletes an Cluster without deleting the Cluster's runs and jobs. To
// avoid unexpected behaviors, delete an Cluster's runs and jobs before
// deleting the Cluster.
func (s *ClusterServer) DeleteCluster(ctx context.Context, request *api.DeleteClusterRequest) (*empty.Empty, error) {
	// TODO: do we want to have some logics here to check cluster exist here? or put it inside resourceManager
	if err := s.resourceManager.DeleteCluster(ctx, request.Id, request.Name); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// Archives an Cluster and the Cluster's runs and jobs.
func (s *ClusterServer) ArchiveCluster(ctx context.Context, request *api.ArchiveClusterRequest) (*empty.Empty, error) {
	if err := s.resourceManager.ArchiveCluster(ctx, request.Id); err != nil {
		return nil, err
	}
	return &empty.Empty{}, nil
}

// Restores an archived Cluster. The Cluster's archived runs and jobs
// will stay archived.
func (s *ClusterServer) UnarchiveCluster(ctx context.Context, request *api.UnarchiveClusterRequest) (*empty.Empty, error) {
	if err := s.resourceManager.UnarchiveCluster(ctx, request.Id); err != nil {
		return nil, err
	}
	return &empty.Empty{}, nil
}

func ValidateCreateClusterRequest(request *api.CreateClusterRequest) error {
	if request.Cluster.Name == "" {
		return util.NewInvalidInputError("Cluster name is empty. Please specify a valid value.")
	}

	if request.Cluster.User == "" {
		return util.NewInvalidInputError("User who create the cluster is empty. Please specify a valid value.")
	}

	if request.Cluster.ClusterRuntime == "" {
		return util.NewInvalidInputError("Cluster runtime is empty. Please specify a valid value.")
	}

	if request.Cluster.ComputeRuntime == "" {
		return util.NewInvalidInputError("Cluster compute runtime is empty. Please specify a valid value.")
	}

	return nil
}

func NewClusterServer(resourceManager *manager.ResourceManager, options *ClusterServerOptions) *ClusterServer {
	return &ClusterServer{resourceManager: resourceManager, options: options}
}
