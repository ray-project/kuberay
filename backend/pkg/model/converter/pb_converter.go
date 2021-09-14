package converter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golang/protobuf/ptypes/timestamp"
	api "github.com/ray-project/kuberay/api/go_client"
	"github.com/ray-project/kuberay/backend/pkg/util"
	"github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	"google.golang.org/protobuf/encoding/protojson"
	v1 "k8s.io/api/core/v1"
)

func FromPbToApiCluster(cluster *v1alpha1.RayCluster) *api.Cluster {
	pbCluster := &api.Cluster{
		Id:          cluster.Name,
		Name:        cluster.Labels[util.RayClusterNameLabelKey],
		Namespace:   cluster.Namespace,
		Version:     cluster.Labels[util.RayClusterVersionLabelKey],
		User:        cluster.Labels[util.RayClusterUserLabelKey],
		Environment: api.Cluster_Environment(api.Cluster_Environment_value[cluster.Labels[util.RayClusterEnvironmentLabelKey]]),
		CreatedAt:   &timestamp.Timestamp{Seconds: cluster.CreationTimestamp.Unix()},
	}

	// loop container and find the resource
	pbCluster.ComputeRuntime = cluster.Labels[util.RayClusterComputeRuntimeTemplateLabelKey]
	pbCluster.ClusterRuntime = cluster.Labels[util.RayClusterClusterRuntimeTemplateLabelKey]

	return pbCluster
}

func PopulateWorkerNodeSpec(specs []v1alpha1.WorkerGroupSpec) []*api.WorkerGroupSpec {
	var workerNodeSpecs []*api.WorkerGroupSpec

	for _, spec := range specs {
		workerNodeSpec := &api.WorkerGroupSpec{
			RayStartParams: spec.RayStartParams,
			MaxReplicas:    *spec.MinReplicas,
			MinReplicas:    *spec.MaxReplicas,
			GroupName:      spec.GroupName,
		}
		// Resources.
		workerNodeSpecs = append(workerNodeSpecs, workerNodeSpec)
	}

	return workerNodeSpecs
}

func FromPbToApiClusters(clusters []*v1alpha1.RayCluster) []*api.Cluster {
	apiClusters := make([]*api.Cluster, 0)
	for _, cluster := range clusters {
		apiClusters = append(apiClusters, FromPbToApiCluster(cluster))
	}
	return apiClusters
}

func PopulateHeadNodeSpec(spec v1alpha1.HeadGroupSpec) *api.HeadGroupSpec {
	headNodeSpec := &api.HeadGroupSpec{
		RayStartParams: spec.RayStartParams,
		ServiceType:    string(spec.ServiceType),
	}

	// we don't have to search pod template. just retrieve number from labels
	// this is a range?
	return headNodeSpec
}

func FromKubeToAPIClusterRuntime(configMap *v1.ConfigMap) *api.ClusterRuntime {
	runtime := &api.ClusterRuntime{}
	runtime.Id = configMap.Name
	runtime.Name = configMap.Labels["ray.io/cluster-runtime"]
	runtime.BaseImage = configMap.Data["base_image"]
	runtime.Image = configMap.Data["image"]
	runtime.CustomCommands = configMap.Data["custom_commands"]

	if len(configMap.Data["pip_packages"]) != 0 {
		runtime.PipPackages = strings.Split(configMap.Data["pip_packages"], ",")
	}

	if len(configMap.Data["conda_packages"]) != 0 {
		runtime.CondaPackages = strings.Split(configMap.Data["conda_packages"], ",")
	}

	if len(configMap.Data["system_packages"]) != 0 {
		runtime.SystemPackages = strings.Split(configMap.Data["system_packages"], ",")
	}

	if len(configMap.Data["environment_variables"]) != 0 {
		var envs map[string]string
		json.Unmarshal([]byte(configMap.Data["environment_variables"]), &envs)
		runtime.EnvironmentVariables = envs
	}

	return runtime
}

func FromKubeToAPIClusterRuntimes(configMaps []*v1.ConfigMap) []*api.ClusterRuntime {
	apiRuntimes := make([]*api.ClusterRuntime, 0)
	for _, configMap := range configMaps {
		apiRuntimes = append(apiRuntimes, FromKubeToAPIClusterRuntime(configMap))
	}
	return apiRuntimes
}

func FromKubeToAPIComputeRuntime(configMap *v1.ConfigMap) *api.ComputeRuntime {
	runtime := &api.ComputeRuntime{}
	runtime.Id = configMap.Name
	//runtime.Name = configMap.Data["name"]
	runtime.Name = configMap.Labels["ray.io/compute-runtime"]
	runtime.Cloud = api.ComputeRuntime_Cloud(api.ComputeRuntime_Cloud_value[configMap.Data["cloud"]])
	runtime.Region = configMap.Data["region"]
	runtime.AvailabilityZone = configMap.Data["availability_zone"]

	headNodeSpec := &api.HeadGroupSpec{}
	protojson.Unmarshal([]byte(configMap.Data["head_node_spec"]), headNodeSpec)
	runtime.HeadGroupSpec = headNodeSpec

	var workerSpecInterfaces []interface{}
	var workerNodeSpecs []*api.WorkerGroupSpec
	fmt.Println(configMap.Data["worker_node_specs"])
	json.Unmarshal([]byte(configMap.Data["worker_node_specs"]), &workerSpecInterfaces)
	for _, workerSpecStr := range workerSpecInterfaces {
		specBytes, err := json.Marshal(workerSpecStr)
		if err != nil {
			return nil
		}
		workerNodeSpec := &api.WorkerGroupSpec{}
		protojson.Unmarshal(specBytes, workerNodeSpec)
		workerNodeSpecs = append(workerNodeSpecs, workerNodeSpec)
	}
	runtime.WorkerGroupSepc = workerNodeSpecs

	return runtime
}

func FromKubeToAPIComputeRuntimes(configMaps []*v1.ConfigMap) []*api.ComputeRuntime {
	apiRuntimes := make([]*api.ComputeRuntime, 0)
	for _, configMap := range configMaps {
		apiRuntimes = append(apiRuntimes, FromKubeToAPIComputeRuntime(configMap))
	}
	return apiRuntimes
}
