package util

import (
	"encoding/base64"

	api "github.com/ray-project/kuberay/proto/go_client"
	rayalphaapi "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const rayServiceDefaultVersion = "2.0.0"

type RayService struct {
	*rayalphaapi.RayService
}

func (s *RayService) Get() *rayalphaapi.RayService {
	return s.RayService
}

func NewRayService(apiService *api.RayService, computeTemplateMap map[string]*api.ComputeTemplate) *RayService {
	rayService := &rayalphaapi.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:        apiService.Name,
			Namespace:   apiService.Namespace,
			Labels:      buildRayServiceLabels(apiService),
			Annotations: buildRayServiceAnnotations(apiService),
		},
		Spec: *buildRayServiceSpec(apiService, computeTemplateMap),
	}
	return &RayService{rayService}
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

func buildRayServiceSpec(apiService *api.RayService, computeTemplateMap map[string]*api.ComputeTemplate) *rayalphaapi.RayServiceSpec {
	serveConfigSpecs := make([]rayalphaapi.ServeConfigSpec, 0)
	for _, serveConfig := range apiService.ServeDeploymentGraphSpec.ServeConfigs {
		serveConfigSpec := rayalphaapi.ServeConfigSpec{
			Name:                 serveConfig.DeploymentName,
			NumReplicas:          &serveConfig.Replicas,
			MaxConcurrentQueries: &serveConfig.MaxConcurrentQueries,
			RoutePrefix:          serveConfig.RoutePrefix,
			UserConfig:           serveConfig.UserConfig,
			AutoscalingConfig:    serveConfig.AutoscalingConfig,
			RayActorOptions: rayalphaapi.RayActorOptionSpec{
				RuntimeEnv:        serveConfig.ActorOptions.RuntimeEnv,
				NumCpus:           &serveConfig.ActorOptions.CpusPerActor,
				NumGpus:           &serveConfig.ActorOptions.GpusPerActor,
				Memory:            &serveConfig.ActorOptions.MemoryPerActor,
				ObjectStoreMemory: &serveConfig.ActorOptions.ObjectStoreMemoryPerActor,
				Resources:         serveConfig.ActorOptions.CustomResource,
				AcceleratorType:   serveConfig.ActorOptions.AccceleratorType,
			},
		}
		serveConfigSpecs = append(serveConfigSpecs, serveConfigSpec)
	}
	return &rayalphaapi.RayServiceSpec{
		ServeDeploymentGraphSpec: rayalphaapi.ServeDeploymentGraphSpec{
			ImportPath:       apiService.ServeDeploymentGraphSpec.ImportPath,
			RuntimeEnv:       base64.StdEncoding.EncodeToString([]byte(apiService.ServeDeploymentGraphSpec.RuntimeEnv)),
			ServeConfigSpecs: serveConfigSpecs,
		},
		RayClusterSpec: *buildRayClusterSpec(rayServiceDefaultVersion, nil, apiService.ClusterSpec, computeTemplateMap),
	}
}

func UpdateRayServiceWorkerGroupSpecs(updateSpecs []*api.WorkerGroupUpdateSpec, workerGroupSpecs []rayalphaapi.WorkerGroupSpec) []rayalphaapi.WorkerGroupSpec {
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

func updateWorkerGroupSpec(updateSpec *api.WorkerGroupUpdateSpec, workerGroupSpec rayalphaapi.WorkerGroupSpec) rayalphaapi.WorkerGroupSpec {
	replicas := updateSpec.Replicas
	minReplicas := updateSpec.MinReplicas
	maxReplicas := updateSpec.MaxReplicas

	workerGroupSpec.Replicas = &replicas
	workerGroupSpec.MinReplicas = &minReplicas
	workerGroupSpec.MaxReplicas = &maxReplicas
	return workerGroupSpec
}

func UpdateServeDeploymentGraphSpec(updateSpecs *api.ServeDeploymentGraphSpec, serveDeploymentGraphspec rayalphaapi.ServeDeploymentGraphSpec) rayalphaapi.ServeDeploymentGraphSpec {
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

func updateServeConfigSpec(updateSpec *api.ServeConfig, serveConfigSpec rayalphaapi.ServeConfigSpec) rayalphaapi.ServeConfigSpec {
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
