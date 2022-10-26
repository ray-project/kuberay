package server

import (
	"context"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/model"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"google.golang.org/protobuf/types/known/emptypb"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type ServiceServerOptions struct {
	CollectMetrics bool
}

// implements `type RayServeServiceServer interface` in serve_grpc.pb.go
// RayServiceServer is the server API for RayServeService service.
type RayServiceServer struct {
	resourceManager *manager.ResourceManager
	options         *ServiceServerOptions
	api.UnimplementedRayServeServiceServer
}

func NewRayServiceServer(resourceManager *manager.ResourceManager, options *ServiceServerOptions) *RayServiceServer {
	return &RayServiceServer{resourceManager: resourceManager, options: options}
}

// Create a new Ray Service
func (s *RayServiceServer) CreateRayService(ctx context.Context, request *api.CreateRayServiceRequest) (*api.RayService, error) {
	if err := ValidateCreateServiceRequest(request); err != nil {
		return nil, util.Wrap(err, "Validate create service request failed.")
	}

	request.Service.Namespace = request.Namespace

	rayService, err := s.resourceManager.CreateService(ctx, request.Service)
	if err != nil {
		return nil, util.Wrap(err, "Create ray service failed.")
	}
	events, err := s.resourceManager.GetServiceEvents(ctx, *rayService)
	if err != nil {
		klog.Warningf("failed to get rayService's event, service: %s/%s, err: %v", rayService.Namespace, rayService.Name, err)
	}
	return model.FromCrdToApiService(rayService, events), nil
}

func (s *RayServiceServer) UpdateRayService(ctx context.Context, request *api.UpdateRayServiceRequest) (*api.RayService, error) {
	if err := ValidateUpdateServiceRequest(request); err != nil {
		return nil, util.Wrap(err, "Validate update service request failed.")
	}
	request.Service.Namespace = request.Namespace

	rayService, err := s.resourceManager.UpdateRayService(ctx, request.Service)
	if err != nil {
		return nil, util.Wrap(err, "Update ray service failed.")
	}
	events, err := s.resourceManager.GetServiceEvents(ctx, *rayService)
	if err != nil {
		klog.Warningf("failed to get rayService's event, service: %s/%s, err: %v", rayService.Namespace, rayService.Name, err)
	}
	return model.FromCrdToApiService(rayService, events), nil
}

func (s *RayServiceServer) UpdateRayServiceConfigs(ctx context.Context, request *api.UpdateRayServiceConfigsRequest) (*api.RayService, error) {
	if err := ValidateUpdateRayServiceConfigsRequest(request); err != nil {
		return nil, err
	}
	service, err := s.resourceManager.UpdateRayServiceConfigs(ctx, request)
	if err != nil {
		return nil, err
	}
	events, err := s.resourceManager.GetServiceEvents(ctx, *service)
	if err != nil {
		klog.Warningf("failed to get rayService's event, service: %s/%s, err: %v", service.Namespace, service.Name, err)
	}
	return model.FromCrdToApiService(service, events), nil
}

func (s *RayServiceServer) GetRayService(ctx context.Context, request *api.GetRayServiceRequest) (*api.RayService, error) {
	if request.Name == "" {
		return nil, util.NewInvalidInputError("ray service name is empty. Please specify a valid value.")
	}

	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("ray service namespace is empty. Please specify a valid value.")
	}
	service, err := s.resourceManager.GetService(ctx, request.Name, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "get ray service failed")
	}
	events, err := s.resourceManager.GetServiceEvents(ctx, *service)
	if err != nil {
		klog.Warningf("failed to get rayService's event, service: %s/%s, err: %v", service.Namespace, service.Name, err)
	}
	return model.FromCrdToApiService(service, events), nil
}

func (s *RayServiceServer) ListRayServices(ctx context.Context, request *api.ListRayServicesRequest) (*api.ListRayServicesResponse, error) {
	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("ray service namespace is empty. Please specify a valid value.")
	}
	services, err := s.resourceManager.ListServices(ctx, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "failed to list rayservice.")
	}
	serviceEventMap := make(map[string][]v1.Event)
	for _, service := range services {
		serviceEvents, err := s.resourceManager.GetServiceEvents(ctx, *service)
		if err != nil {
			klog.Warningf("Failed to get cluster's event, cluster: %s/%s, err: %v", service.Namespace, service.Name, err)
			continue
		}
		serviceEventMap[service.Name] = serviceEvents
	}
	return &api.ListRayServicesResponse{
		Services: model.FromCrdToApiServices(services, serviceEventMap),
	}, nil
}

func (s *RayServiceServer) ListAllRayServices(ctx context.Context, request *api.ListAllRayServicesRequest) (*api.ListAllRayServicesResponse, error) {
	services, err := s.resourceManager.ListAllServices(ctx)
	if err != nil {
		return nil, util.Wrap(err, "list all services failed.")
	}
	serviceEventMap := make(map[string][]v1.Event)
	for _, service := range services {
		serviceEvents, err := s.resourceManager.GetServiceEvents(ctx, *service)
		if err != nil {
			klog.Warningf("Failed to get cluster's event, cluster: %s/%s, err: %v", service.Namespace, service.Name, err)
			continue
		}
		serviceEventMap[service.Name] = serviceEvents
	}
	return &api.ListAllRayServicesResponse{
		Services: model.FromCrdToApiServices(services, serviceEventMap),
	}, nil
}

func (s *RayServiceServer) DeleteRayService(ctx context.Context, request *api.DeleteRayServiceRequest) (*emptypb.Empty, error) {
	if request.Name == "" {
		return nil, util.NewInvalidInputError("ray service name is empty. Please specify a valid value.")
	}

	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("ray service namespace is empty. Please specify a valid value.")
	}
	if err := s.resourceManager.DeleteCluster(ctx, request.Name, request.Namespace); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func ValidateCreateServiceRequest(request *api.CreateRayServiceRequest) error {
	if request.Namespace == "" {
		return util.NewInvalidInputError("Namespace is empty. Please specify a valid value.")
	}

	if request.Service == nil {
		return util.NewInvalidInputError("Service is empty, please input a valid payload.")
	}

	if request.Namespace != request.Service.Namespace {
		return util.NewInvalidInputError("The namespace in the request is different from the namespace in the service definition.")
	}

	if request.Service.Name == "" {
		return util.NewInvalidInputError("Service name is empty. Please specify a valid value.")
	}

	if request.Service.User == "" {
		return util.NewInvalidInputError("User who create the Service is empty. Please specify a valid value.")
	}

	if len(request.Service.ClusterSpec.HeadGroupSpec.ComputeTemplate) == 0 {
		return util.NewInvalidInputError("HeadGroupSpec compute template is empty. Please specify a valid value.")
	}

	for index, spec := range request.Service.ClusterSpec.WorkerGroupSpec {
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

func ValidateUpdateServiceRequest(request *api.UpdateRayServiceRequest) error {
	if request.Name == "" {
		return util.NewInvalidInputError("Service name is empty. Please specify a valid value.")
	}
	if request.Namespace == "" {
		return util.NewInvalidInputError("Namespace is empty. Please specify a valid value.")
	}

	if request.Service == nil {
		return util.NewInvalidInputError("Service is empty, please input a valid payload.")
	}

	if request.Namespace != request.Service.Namespace {
		return util.NewInvalidInputError("The namespace in the request is different from the namespace in the service definition.")
	}

	if request.Service.Name == "" {
		return util.NewInvalidInputError("Service name is empty. Please specify a valid value.")
	}

	if request.Service.User == "" {
		return util.NewInvalidInputError("User who create the Service is empty. Please specify a valid value.")
	}

	if len(request.Service.ClusterSpec.HeadGroupSpec.ComputeTemplate) == 0 {
		return util.NewInvalidInputError("HeadGroupSpec compute template is empty. Please specify a valid value.")
	}

	for index, spec := range request.Service.ClusterSpec.WorkerGroupSpec {
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

func ValidateUpdateRayServiceConfigsRequest(request *api.UpdateRayServiceConfigsRequest) error {
	if request.Name == "" {
		return util.NewInvalidInputError("ray service name is empty. Please specify a valid value.")
	}
	if request.Namespace == "" {
		return util.NewInvalidInputError("ray service namespace is empty. Please specify a valid value.")
	}
	updateServiceBody := request.GetUpdateService()
	if updateServiceBody == nil || (updateServiceBody.WorkerGroupUpdateSpec == nil && updateServiceBody.ServeDeploymentGraphSpec == nil) {
		return util.NewInvalidInputError("update spec is empty. Nothing to update.")
	}
	if updateServiceBody.WorkerGroupUpdateSpec != nil {
		for _, spec := range updateServiceBody.WorkerGroupUpdateSpec {
			if spec.Replicas <= 0 || spec.MinReplicas <= 0 || spec.MaxReplicas <= 0 {
				return util.NewInvalidInputError("input invalid, replicas, minReplicas and maxReplicas must be greater than 0.")
			}
			if spec.MinReplicas > spec.MaxReplicas {
				return util.NewInvalidInputError("WorkerNodeSpec %s MinReplica > MaxReplicas. Please specify a valid value.", spec.GroupName)
			}
		}
	}
	if updateServiceBody.ServeDeploymentGraphSpec != nil {
		for _, spec := range updateServiceBody.ServeDeploymentGraphSpec.ServeConfigs {
			if spec.Replicas <= 0 {
				return util.NewInvalidInputError("input invalid, replicas must be greater than 0.")
			}
			if spec.ActorOptions != nil {
				if spec.ActorOptions.CpusPerActor <= 0 && spec.ActorOptions.GpusPerActor <= 0 && spec.ActorOptions.MemoryPerActor <= 0 {
					return util.NewInvalidInputError("input invalid, cpusPerActor, gpusPerActor and memoryPerActor must be greater than 0.")
				}
			}
		}
	}
	return nil
}
