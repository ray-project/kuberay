package model

import (
	"encoding/base64"
	"fmt"
	"strconv"

	"k8s.io/klog/v2"

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	v1 "k8s.io/api/core/v1"
)

func FromCrdToApiClusters(clusters []*v1alpha1.RayCluster, clusterEventsMap map[string][]v1.Event) []*api.Cluster {
	apiClusters := make([]*api.Cluster, 0)
	for _, cluster := range clusters {
		apiClusters = append(apiClusters, FromCrdToApiCluster(cluster, clusterEventsMap[cluster.Name]))
	}
	return apiClusters
}

func FromCrdToApiCluster(cluster *v1alpha1.RayCluster, events []v1.Event) *api.Cluster {
	pbCluster := &api.Cluster{
		Name:         cluster.Name,
		Namespace:    cluster.Namespace,
		Version:      cluster.Labels[util.RayClusterVersionLabelKey],
		User:         cluster.Labels[util.RayClusterUserLabelKey],
		Environment:  api.Cluster_Environment(api.Cluster_Environment_value[cluster.Labels[util.RayClusterEnvironmentLabelKey]]),
		CreatedAt:    &timestamp.Timestamp{Seconds: cluster.CreationTimestamp.Unix()},
		ClusterState: string(cluster.Status.State),
	}

	// loop container and find the resource
	pbCluster.ClusterSpec = PopulateRayClusterSpec(cluster.Spec)

	// parse events
	for _, event := range events {
		clusterEvent := &api.ClusterEvent{
			Id:             event.Name,
			Name:           fmt.Sprintf("%s-%s", cluster.Labels[util.RayClusterNameLabelKey], event.Name),
			CreatedAt:      &timestamp.Timestamp{Seconds: event.ObjectMeta.CreationTimestamp.Unix()},
			FirstTimestamp: &timestamp.Timestamp{Seconds: event.FirstTimestamp.Unix()},
			LastTimestamp:  &timestamp.Timestamp{Seconds: event.LastTimestamp.Unix()},
			Reason:         event.Reason,
			Message:        event.Message,
			Type:           event.Type,
			Count:          event.Count,
		}
		pbCluster.Events = append(pbCluster.Events, clusterEvent)
	}

	pbCluster.ServiceEndpoint = map[string]string{}
	for name, port := range cluster.Status.Endpoints {
		pbCluster.ServiceEndpoint[name] = port
	}
	return pbCluster
}

func PopulateRayClusterSpec(spec v1alpha1.RayClusterSpec) *api.ClusterSpec {
	clusterSpec := &api.ClusterSpec{}
	clusterSpec.HeadGroupSpec = PopulateHeadNodeSpec(spec.HeadGroupSpec)
	clusterSpec.WorkerGroupSpec = PopulateWorkerNodeSpec(spec.WorkerGroupSpecs)
	return clusterSpec
}

func PopulateHeadNodeSpec(spec v1alpha1.HeadGroupSpec) *api.HeadGroupSpec {
	headNodeSpec := &api.HeadGroupSpec{
		RayStartParams:  spec.RayStartParams,
		ServiceType:     string(spec.ServiceType),
		Image:           spec.Template.Annotations[util.RayClusterImageAnnotationKey],
		ComputeTemplate: spec.Template.Annotations[util.RayClusterComputeTemplateAnnotationKey],
	}

	return headNodeSpec
}

func PopulateWorkerNodeSpec(specs []v1alpha1.WorkerGroupSpec) []*api.WorkerGroupSpec {
	var workerNodeSpecs []*api.WorkerGroupSpec

	for _, spec := range specs {
		workerNodeSpec := &api.WorkerGroupSpec{
			RayStartParams:  spec.RayStartParams,
			MaxReplicas:     *spec.MinReplicas,
			MinReplicas:     *spec.MaxReplicas,
			Replicas:        *spec.Replicas,
			GroupName:       spec.GroupName,
			Image:           spec.Template.Annotations[util.RayClusterImageAnnotationKey],
			ComputeTemplate: spec.Template.Annotations[util.RayClusterComputeTemplateAnnotationKey],
		}
		// Resources.
		workerNodeSpecs = append(workerNodeSpecs, workerNodeSpec)
	}

	return workerNodeSpecs
}

func FromKubeToAPIComputeTemplate(configMap *v1.ConfigMap) *api.ComputeTemplate {
	cpu, _ := strconv.ParseUint(configMap.Data["cpu"], 10, 32)
	memory, _ := strconv.ParseUint(configMap.Data["memory"], 10, 32)
	gpu, _ := strconv.ParseUint(configMap.Data["gpu"], 10, 32)

	runtime := &api.ComputeTemplate{}
	runtime.Name = configMap.Name
	runtime.Namespace = configMap.Namespace
	runtime.Cpu = uint32(cpu)
	runtime.Memory = uint32(memory)
	runtime.Gpu = uint32(gpu)
	runtime.GpuAccelerator = configMap.Data["gpu_accelerator"]
	return runtime
}

func FromKubeToAPIComputeTemplates(configMaps []*v1.ConfigMap) []*api.ComputeTemplate {
	apiComputeTemplates := make([]*api.ComputeTemplate, 0)
	for _, configMap := range configMaps {
		apiComputeTemplates = append(apiComputeTemplates, FromKubeToAPIComputeTemplate(configMap))
	}
	return apiComputeTemplates
}

func FromCrdToApiJobs(jobs []*v1alpha1.RayJob) []*api.RayJob {
	apiJobs := make([]*api.RayJob, 0)
	for _, job := range jobs {
		apiJobs = append(apiJobs, FromCrdToApiJob(job))
	}
	return apiJobs
}

func FromCrdToApiJob(job *v1alpha1.RayJob) (pbJob *api.RayJob) {
	defer func() {
		err := recover()
		if err != nil {
			klog.Errorf("failed to transfer job crd to job protobuf, err: %v, crd: %+v", err, job)
		}
	}()

	pbJob = &api.RayJob{
		Name:                     job.Name,
		Namespace:                job.Namespace,
		User:                     job.Labels[util.RayClusterUserLabelKey],
		Entrypoint:               job.Spec.Entrypoint,
		Metadata:                 job.Spec.Metadata,
		RuntimeEnv:               job.Spec.RuntimeEnv,
		JobId:                    job.Status.JobId,
		ShutdownAfterJobFinishes: job.Spec.ShutdownAfterJobFinishes,
		ClusterSelector:          job.Spec.ClusterSelector,
		CreatedAt:                &timestamp.Timestamp{Seconds: job.CreationTimestamp.Unix()},
		JobStatus:                string(job.Status.JobStatus),
		JobDeploymentStatus:      string(job.Status.JobDeploymentStatus),
		Message:                  job.Status.Message,
	}

	if job.Spec.RayClusterSpec != nil {
		pbJob.ClusterSpec = PopulateRayClusterSpec(*job.Spec.RayClusterSpec)
	}

	if job.Spec.TTLSecondsAfterFinished != nil {
		pbJob.TtlSecondsAfterFinished = *job.Spec.TTLSecondsAfterFinished
	}

	if job.DeletionTimestamp != nil {
		pbJob.DeleteAt = &timestamp.Timestamp{Seconds: job.DeletionTimestamp.Unix()}
	}

	return pbJob
}

func FromCrdToApiServices(services []*v1alpha1.RayService, serviceEventsMap map[string][]v1.Event) []*api.RayService {
	apiServices := make([]*api.RayService, 0)
	for _, service := range services {
		apiServices = append(apiServices, FromCrdToApiService(service, serviceEventsMap[service.Name]))
	}
	return apiServices
}

func FromCrdToApiService(service *v1alpha1.RayService, events []v1.Event) *api.RayService {
	defer func() {
		err := recover()
		if err != nil {
			klog.Errorf("failed to transfer ray service, err: %v, item: %v", err, service)
		}
	}()

	var deleteTime int64 = -1
	if service.DeletionTimestamp != nil {
		deleteTime = service.DeletionTimestamp.Unix()
	}
	pbService := &api.RayService{
		Name:                     service.Name,
		Namespace:                service.Namespace,
		User:                     service.Labels[util.RayClusterUserLabelKey],
		ServeDeploymentGraphSpec: PopulateServeDeploymentGraphSpec(service.Spec.ServeDeploymentGraphSpec),
		ClusterSpec:              PopulateRayClusterSpec(service.Spec.RayClusterSpec),
		RayServiceStatus:         PoplulateRayServiceStatus(service.Name, service.Status, events),
		CreatedAt:                &timestamp.Timestamp{Seconds: service.CreationTimestamp.Unix()},
		DeleteAt:                 &timestamp.Timestamp{Seconds: deleteTime},
	}
	return pbService
}

func PopulateServeDeploymentGraphSpec(spec v1alpha1.ServeDeploymentGraphSpec) *api.ServeDeploymentGraphSpec {
	runtimeEnv, _ := base64.StdEncoding.DecodeString(spec.RuntimeEnv)
	return &api.ServeDeploymentGraphSpec{
		ImportPath:   spec.ImportPath,
		RuntimeEnv:   string(runtimeEnv),
		ServeConfigs: PopulateServeConfig(spec.ServeConfigSpecs),
	}
}

func PopulateServeConfig(serveConfigSpecs []v1alpha1.ServeConfigSpec) []*api.ServeConfig {
	serveConfigs := make([]*api.ServeConfig, 0)
	for _, serveConfigSpec := range serveConfigSpecs {
		serveConfig := &api.ServeConfig{
			DeploymentName:       serveConfigSpec.Name,
			Replicas:             *serveConfigSpec.NumReplicas,
			RoutePrefix:          serveConfigSpec.RoutePrefix,
			MaxConcurrentQueries: *serveConfigSpec.MaxConcurrentQueries,
			UserConfig:           serveConfigSpec.UserConfig,
			AutoscalingConfig:    serveConfigSpec.AutoscalingConfig,
			ActorOptions: &api.ActorOptions{
				RuntimeEnv:                serveConfigSpec.RayActorOptions.RuntimeEnv,
				CpusPerActor:              *serveConfigSpec.RayActorOptions.NumCpus,
				GpusPerActor:              *serveConfigSpec.RayActorOptions.NumGpus,
				MemoryPerActor:            *serveConfigSpec.RayActorOptions.Memory,
				ObjectStoreMemoryPerActor: *serveConfigSpec.RayActorOptions.ObjectStoreMemory,
				CustomResource:            serveConfigSpec.RayActorOptions.Resources,
				AccceleratorType:          serveConfigSpec.RayActorOptions.AcceleratorType,
			},
		}
		serveConfigs = append(serveConfigs, serveConfig)
	}
	return serveConfigs
}

func PoplulateRayServiceStatus(serviceName string, serviceStatus v1alpha1.RayServiceStatuses, events []v1.Event) *api.RayServiceStatus {
	status := &api.RayServiceStatus{
		ApplicationStatus:     serviceStatus.ActiveServiceStatus.ApplicationStatus.Status,
		ApplicationMessage:    serviceStatus.ActiveServiceStatus.ApplicationStatus.Message,
		ServeDeploymentStatus: PopulateServeDeploymentStatus(serviceStatus.ActiveServiceStatus.ServeStatuses),
		RayServiceEvents:      PopulateRayServiceEvent(serviceName, events),
		RayClusterName:        serviceStatus.ActiveServiceStatus.RayClusterName,
		RayClusterState:       string(serviceStatus.ActiveServiceStatus.RayClusterStatus.State),
	}
	status.ServiceEndpoint = map[string]string{}
	for name, port := range serviceStatus.ActiveServiceStatus.RayClusterStatus.Endpoints {
		status.ServiceEndpoint[name] = port
	}
	return status
}

func PopulateServeDeploymentStatus(serveDeploymentStatuses []v1alpha1.ServeDeploymentStatus) []*api.ServeDeploymentStatus {
	deploymentStatus := make([]*api.ServeDeploymentStatus, 0)
	for _, serveDeploymentStatus := range serveDeploymentStatuses {
		ds := &api.ServeDeploymentStatus{
			DeploymentName: serveDeploymentStatus.Name,
			Status:         serveDeploymentStatus.Status,
			Message:        serveDeploymentStatus.Message,
		}
		deploymentStatus = append(deploymentStatus, ds)
	}
	return deploymentStatus
}

func PopulateRayServiceEvent(serviceName string, events []v1.Event) []*api.RayServiceEvent {
	serviceEvents := make([]*api.RayServiceEvent, 0)
	for _, event := range events {
		serviceEvent := &api.RayServiceEvent{
			Id:             event.Name,
			Name:           fmt.Sprintf("%s-%s", serviceName, event.Name),
			CreatedAt:      &timestamp.Timestamp{Seconds: event.ObjectMeta.CreationTimestamp.Unix()},
			FirstTimestamp: &timestamp.Timestamp{Seconds: event.FirstTimestamp.Unix()},
			LastTimestamp:  &timestamp.Timestamp{Seconds: event.LastTimestamp.Unix()},
			Reason:         event.Reason,
			Message:        event.Message,
			Type:           event.Type,
			Count:          event.Count,
		}
		serviceEvents = append(serviceEvents, serviceEvent)
	}
	return serviceEvents
}
