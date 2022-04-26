package model

import (
	"strconv"

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	v1 "k8s.io/api/core/v1"
)

func FromCrdToApiClusters(clusters []*v1alpha1.RayCluster) []*api.Cluster {
	apiClusters := make([]*api.Cluster, 0)
	for _, cluster := range clusters {
		apiClusters = append(apiClusters, FromCrdToApiCluster(cluster))
	}
	return apiClusters
}

func FromCrdToApiCluster(cluster *v1alpha1.RayCluster) *api.Cluster {
	pbCluster := &api.Cluster{
		Name:        cluster.Name,
		Namespace:   cluster.Namespace,
		Version:     cluster.Labels[util.RayClusterVersionLabelKey],
		User:        cluster.Labels[util.RayClusterUserLabelKey],
		Environment: api.Cluster_Environment(api.Cluster_Environment_value[cluster.Labels[util.RayClusterEnvironmentLabelKey]]),
		CreatedAt:   &timestamp.Timestamp{Seconds: cluster.CreationTimestamp.Unix()},
	}

	// loop container and find the resource
	pbCluster.ClusterSpec = &api.ClusterSpec{}
	pbCluster.ClusterSpec.HeadGroupSpec = PopulateHeadNodeSpec(cluster.Spec.HeadGroupSpec)
	pbCluster.ClusterSpec.WorkerGroupSepc = PopulateWorkerNodeSpec(cluster.Spec.WorkerGroupSpecs)

	return pbCluster
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
