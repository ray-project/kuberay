package server

import (
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/model"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
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
	if err := ValidateCreateClusterRequest(request); err != nil {
		return nil, util.Wrap(err, "Validate cluster request failed.")
	}

	// use the namespace in the request to override the namespace in the cluster definition
	request.Cluster.Namespace = request.Namespace

	cluster, err := s.resourceManager.CreateCluster(ctx, request.Cluster)
	if err != nil {
		return nil, util.Wrap(err, "Create Cluster failed.")
	}

	return model.FromCrdToApiCluster(cluster), nil
}

// Finds a specific Cluster by cluster name.
func (s *ClusterServer) GetCluster(ctx context.Context, request *api.GetClusterRequest) (*api.Cluster, error) {
	cluster, err := s.resourceManager.GetCluster(ctx, request.Name, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "Get cluster failed.")
	}
	return model.FromCrdToApiCluster(cluster), nil
}

// Finds all Clusters.
// TODO: Supports pagination and sorting on certain fields when we have DB support. request needs to be extended.
func (s *ClusterServer) ListCluster(ctx context.Context, request *api.ListClustersRequest) (*api.ListClustersResponse, error) {
	clusters, err := s.resourceManager.ListClusters(ctx, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "List clusters failed.")
	}

	return &api.ListClustersResponse{
		Clusters: model.FromCrdToApiClusters(clusters),
	}, nil
}

// Deletes an Cluster without deleting the Cluster's runs and jobs. To
// avoid unexpected behaviors, delete an Cluster's runs and jobs before
// deleting the Cluster.
func (s *ClusterServer) DeleteCluster(ctx context.Context, request *api.DeleteClusterRequest) (*empty.Empty, error) {
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

	if request.Cluster.Name == "" {
		return util.NewInvalidInputError("Cluster name is empty. Please specify a valid value.")
	}

	if request.Cluster.User == "" {
		return util.NewInvalidInputError("User who create the cluster is empty. Please specify a valid value.")
	}

	if len(request.Cluster.ClusterSpec.HeadGroupSpec.ComputeTemplate) == 0 {
		return util.NewInvalidInputError("HeadGroupSpec compute template is empty. Please specify a valid value.")
	}

	for index, spec := range request.Cluster.ClusterSpec.WorkerGroupSepc {
		if len(spec.GroupName) == 0 {
			return util.NewInvalidInputError("WorkerNodeSpec %d group name is empty. Please specify a valid value.", index)
		}
		if len(spec.ComputeTemplate) == 0 {
			return util.NewInvalidInputError("WorkerNodeSpec %d compute template is empty. Please specify a valid value.", index)
		}
		if spec.MaxReplicas == 0 {
			return util.NewInvalidInputError("WorkerNodeSpec %d MaxReplicas can not be 0. Please specify a valid value.", index)
		}
		if spec.MinReplicas > spec.MaxReplicas {
			return util.NewInvalidInputError("WorkerNodeSpec %d MinReplica > MaxReplicas. Please specify a valid value.", index)
		}
	}

	return nil
}

func NewClusterServer(resourceManager *manager.ResourceManager, options *ClusterServerOptions) *ClusterServer {
	return &ClusterServer{resourceManager: resourceManager, options: options}
}
