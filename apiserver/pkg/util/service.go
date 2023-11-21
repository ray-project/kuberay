package util

import (
	"encoding/base64"
	"errors"

	api "github.com/ray-project/kuberay/proto/go_client"
	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	rayServiceDefaultVersion = "2.0.0"
	defaultServePortName     = "serve"
	defaultServePort         = 8000
)

type RayService struct {
	*rayv1api.RayService
}

func (s *RayService) Get() *rayv1api.RayService {
	return s.RayService
}

func NewRayService(apiService *api.RayService, computeTemplateMap map[string]*api.ComputeTemplate) (*RayService, error) {
	// Build the spec
	spec, err := buildRayServiceSpec(apiService, computeTemplateMap)
	if err != nil {
		return nil, err
	}
	// Build Ray service
	rayService := &rayv1api.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:        apiService.Name,
			Namespace:   apiService.Namespace,
			Labels:      buildRayServiceLabels(apiService),
			Annotations: buildRayServiceAnnotations(apiService),
		},
		Spec: *spec,
	}
	return &RayService{rayService}, nil
}

func buildRayServiceLabels(apiService *api.RayService) map[string]string {
	labels := map[string]string{}
	labels[RayServiceLabelKey] = apiService.Name
	labels[RayClusterUserLabelKey] = apiService.User
	labels[KubernetesApplicationNameLabelKey] = ApplicationName
	labels[KubernetesManagedByLabelKey] = ComponentName
	return labels
}

func buildRayServiceAnnotations(apiService *api.RayService) map[string]string {
	annotations := map[string]string{}
	// TODO: Add optional annotations
	return annotations
}

func buildRayServiceSpec(apiService *api.RayService, computeTemplateMap map[string]*api.ComputeTemplate) (*rayv1api.RayServiceSpec, error) {
	// Ensure that at least one and only one serve config (V1 or V2) defined
	if apiService.ServeConfig_V2 == "" && apiService.ServeDeploymentGraphSpec == nil {
		// Serve configuration is not defined
		return nil, errors.New("serve configuration is not defined")
	}

	if apiService.ServeDeploymentGraphSpec != nil && apiService.ServeConfig_V2 != "" {
		// Serve configuration is not defined
		return nil, errors.New("two serve configuration are defined, only one is allowed")
	}
	// generate Ray cluster spec and buid cluster
	newRayClusterSpec, err := buildRayClusterSpec(rayServiceDefaultVersion, nil, apiService.ClusterSpec, computeTemplateMap, true)
	if err != nil {
		return nil, err
	}
	newRayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Ports = append(newRayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Ports, v1.ContainerPort{
		Name:          defaultServePortName,
		ContainerPort: defaultServePort,
	})
	var serviceUnhealthySecondThreshold *int32
	if apiService.ServiceUnhealthySecondThreshold > 0 {
		serviceUnhealthySecondThreshold = &apiService.ServiceUnhealthySecondThreshold
	} else {
		serviceUnhealthySecondThreshold = nil
	}
	var deploymentUnhealthySecondThreshold *int32
	if apiService.DeploymentUnhealthySecondThreshold > 0 {
		deploymentUnhealthySecondThreshold = &apiService.DeploymentUnhealthySecondThreshold
	} else {
		deploymentUnhealthySecondThreshold = nil
	}

	if apiService.ServeDeploymentGraphSpec != nil {
		// V1 definition
		serveConfigSpecs := make([]rayv1api.ServeConfigSpec, 0)
		for _, serveConfig := range apiService.ServeDeploymentGraphSpec.ServeConfigs {
			actorOptions := rayv1api.RayActorOptionSpec{}
			if serveConfig.ActorOptions != nil {
				if serveConfig.ActorOptions.RuntimeEnv != "" {
					actorOptions.RuntimeEnv = serveConfig.ActorOptions.RuntimeEnv
				}
				if serveConfig.ActorOptions.CpusPerActor > 0 {
					actorOptions.NumCpus = &serveConfig.ActorOptions.CpusPerActor
				}
				if serveConfig.ActorOptions.GpusPerActor > 0 {
					actorOptions.NumGpus = &serveConfig.ActorOptions.GpusPerActor
				}
				if serveConfig.ActorOptions.MemoryPerActor > 0 {
					actorOptions.Memory = &serveConfig.ActorOptions.MemoryPerActor
				}
				if serveConfig.ActorOptions.ObjectStoreMemoryPerActor > 0 {
					actorOptions.ObjectStoreMemory = &serveConfig.ActorOptions.ObjectStoreMemoryPerActor
				}
				if serveConfig.ActorOptions.CustomResource != "" {
					actorOptions.Resources = serveConfig.ActorOptions.CustomResource
				}
				if serveConfig.ActorOptions.AccceleratorType != "" {
					actorOptions.AcceleratorType = serveConfig.ActorOptions.AccceleratorType
				}
			}
			serveConfigSpec := rayv1api.ServeConfigSpec{
				Name:            serveConfig.DeploymentName,
				NumReplicas:     &serveConfig.Replicas,
				RayActorOptions: actorOptions,
			}
			if serveConfig.MaxConcurrentQueries > 0 {
				serveConfigSpec.MaxConcurrentQueries = &serveConfig.MaxConcurrentQueries
			}
			if serveConfig.RoutePrefix != "" {
				serveConfigSpec.RoutePrefix = serveConfig.RoutePrefix
			}
			if serveConfig.UserConfig != "" {
				serveConfigSpec.UserConfig = serveConfig.UserConfig
			}
			if serveConfig.AutoscalingConfig != "" {
				serveConfigSpec.AutoscalingConfig = serveConfig.AutoscalingConfig
			}

			serveConfigSpecs = append(serveConfigSpecs, serveConfigSpec)
		}
		return &rayv1api.RayServiceSpec{
			ServeDeploymentGraphSpec: rayv1api.ServeDeploymentGraphSpec{
				ImportPath:       apiService.ServeDeploymentGraphSpec.ImportPath,
				RuntimeEnv:       apiService.ServeDeploymentGraphSpec.RuntimeEnv,
				ServeConfigSpecs: serveConfigSpecs,
			},
			RayClusterSpec:                     *newRayClusterSpec,
			ServiceUnhealthySecondThreshold:    serviceUnhealthySecondThreshold,
			DeploymentUnhealthySecondThreshold: deploymentUnhealthySecondThreshold,
		}, nil
	}
	// V2 definition
	return &rayv1api.RayServiceSpec{
		ServeConfigV2:                      apiService.ServeConfig_V2,
		RayClusterSpec:                     *newRayClusterSpec,
		ServiceUnhealthySecondThreshold:    serviceUnhealthySecondThreshold,
		DeploymentUnhealthySecondThreshold: deploymentUnhealthySecondThreshold,
	}, nil
}

func UpdateRayServiceWorkerGroupSpecs(updateSpecs []*api.WorkerGroupUpdateSpec, workerGroupSpecs []rayv1api.WorkerGroupSpec) []rayv1api.WorkerGroupSpec {
	specMap := map[string]*api.WorkerGroupUpdateSpec{}
	for _, spec := range updateSpecs {
		if spec != nil {
			specMap[spec.GroupName] = spec
		}
	}
	for i, spec := range workerGroupSpecs {
		if updateSpec, ok := specMap[spec.GroupName]; ok {
			newSpec := updateWorkerGroupSpec(updateSpec, spec)
			workerGroupSpecs[i] = newSpec
		}
	}
	return workerGroupSpecs
}

func updateWorkerGroupSpec(updateSpec *api.WorkerGroupUpdateSpec, workerGroupSpec rayv1api.WorkerGroupSpec) rayv1api.WorkerGroupSpec {
	replicas := updateSpec.Replicas
	minReplicas := updateSpec.MinReplicas
	maxReplicas := updateSpec.MaxReplicas

	workerGroupSpec.Replicas = &replicas
	workerGroupSpec.MinReplicas = &minReplicas
	workerGroupSpec.MaxReplicas = &maxReplicas
	return workerGroupSpec
}

func UpdateServeDeploymentGraphSpec(updateSpecs *api.ServeDeploymentGraphSpec, serveDeploymentGraphspec rayv1api.ServeDeploymentGraphSpec) rayv1api.ServeDeploymentGraphSpec {
	if updateSpecs.ImportPath != "" {
		serveDeploymentGraphspec.ImportPath = updateSpecs.ImportPath
	}
	if updateSpecs.RuntimeEnv != "" {
		serveDeploymentGraphspec.RuntimeEnv = base64.StdEncoding.EncodeToString([]byte(updateSpecs.RuntimeEnv))
	}

	if updateSpecs.ServeConfigs != nil {
		specMap := map[string]*api.ServeConfig{}
		for _, spec := range updateSpecs.ServeConfigs {
			if spec != nil {
				specMap[spec.DeploymentName] = spec
			}
		}
		for i, spec := range serveDeploymentGraphspec.ServeConfigSpecs {
			if updateSpec, ok := specMap[spec.Name]; ok {
				newSpec := updateServeConfigSpec(updateSpec, spec)
				serveDeploymentGraphspec.ServeConfigSpecs[i] = newSpec
			}
		}
	}
	return serveDeploymentGraphspec
}

func updateServeConfigSpec(updateSpec *api.ServeConfig, serveConfigSpec rayv1api.ServeConfigSpec) rayv1api.ServeConfigSpec {
	if updateSpec.Replicas != 0 {
		serveConfigSpec.NumReplicas = &updateSpec.Replicas
	}
	if updateSpec.ActorOptions.CpusPerActor != 0 {
		serveConfigSpec.RayActorOptions.NumCpus = &updateSpec.ActorOptions.CpusPerActor
	}
	if updateSpec.ActorOptions.GpusPerActor != 0 {
		serveConfigSpec.RayActorOptions.NumGpus = &updateSpec.ActorOptions.GpusPerActor
	}
	if updateSpec.ActorOptions.MemoryPerActor != 0 {
		serveConfigSpec.RayActorOptions.Memory = &updateSpec.ActorOptions.MemoryPerActor
	}
	return serveConfigSpec
}
