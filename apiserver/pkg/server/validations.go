package server

import (
	"strings"

	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
)

// ValidateClusterSpec validates that the *api.ClusterSpec is not nil and
// has all the required fields
func ValidateClusterSpec(clusterSpec *api.ClusterSpec) error {
	if clusterSpec == nil {
		return util.NewInvalidInputError("A ClusterSpec object is required. Please specify one.")
	}
	if clusterSpec.HeadGroupSpec == nil {
		return util.NewInvalidInputError("Cluster Spec Object requires HeadGroupSpec to be populated. Please specify one.")
	}
	if len(clusterSpec.HeadGroupSpec.ComputeTemplate) == 0 {
		return util.NewInvalidInputError("HeadGroupSpec compute template is empty. Please specify a valid value.")
	}
	if len(clusterSpec.HeadGroupSpec.RayStartParams) == 0 {
		return util.NewInvalidInputError("HeadGroupSpec RayStartParams is empty. Please specify values.")
	}

	for index, spec := range clusterSpec.WorkerGroupSpec {
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

// ValidateServeDeploymentGraphSpec validates that the ServeDeploymentGraphSpec has the CRD required fields
func ValidateServeDeploymentGraphSpec(deploymentGraphSpec *api.ServeDeploymentGraphSpec) error {
	if deploymentGraphSpec == nil {
		return util.NewInvalidInputError("ServeDeploymentGraphSpec must be not nil. Please specify a valid object.")
	}
	if strings.TrimSpace(deploymentGraphSpec.ImportPath) == "" {
		return util.NewInvalidInputError("ServeDeploymentGraphSpec import path must have a value. Please specify valid value.")
	}
	for index, serveConfig := range deploymentGraphSpec.ServeConfigs {
		if strings.TrimSpace(serveConfig.DeploymentName) == "" {
			return util.NewInvalidInputError("ServeConfig %d deployment name is empty. Please specify a valid value.", index)
		}
		if serveConfig.Replicas <= 0 {
			return util.NewInvalidInputError("ServeConfig %d replicas must be greater than 0. Please specify a valid value.", index)
		}
		if serveConfig.ActorOptions != nil {
			if serveConfig.ActorOptions.CpusPerActor <= 0 && serveConfig.ActorOptions.GpusPerActor <= 0 && serveConfig.ActorOptions.MemoryPerActor <= 0 {
				return util.NewInvalidInputError("ServeConfig %d invalid ActorOptions, cpusPerActor, gpusPerActor and memoryPerActor must be greater than 0.", index)
			}
		}
	}
	return nil
}
