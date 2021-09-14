package util

import (
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/ray-project/kuberay/api/go_client"
	rayclusterapi "github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	"google.golang.org/protobuf/encoding/protojson"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RayCluster struct {
	*rayclusterapi.RayCluster
}

// NewRayCluster creates a RayCluster.
func NewRayCluster(apiCluster *api.Cluster, clusterRuntime *api.ClusterRuntime, computeRuntime *api.ComputeRuntime) *RayCluster {
	// figure out how to build this
	headPodTemplate := buildHeadPodTemplate(apiCluster)
	headReplicas := int32(1)
	rayCluster := &rayclusterapi.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        apiCluster.Name,
			Namespace:   apiCluster.Namespace,
			Labels:      buildRayClusterLabels(apiCluster, clusterRuntime, computeRuntime),
			Annotations: buildRayClusterAnnotations(apiCluster, clusterRuntime, computeRuntime),
		},
		Spec: rayclusterapi.RayClusterSpec{
			RayVersion: apiCluster.Version,
			HeadGroupSpec: rayclusterapi.HeadGroupSpec{
				// TODO: verify it's a valid ServiceType
				ServiceType:    v1.ServiceType(computeRuntime.HeadGroupSpec.ServiceType),
				Template:       headPodTemplate,
				Replicas:       &headReplicas,
				RayStartParams: computeRuntime.HeadGroupSpec.RayStartParams,
			},
			WorkerGroupSpecs: []rayclusterapi.WorkerGroupSpec{},
		},
	}

	for _, spec := range computeRuntime.WorkerGroupSepc {
		workerPodTemplate := buildWorkerPodTemplate(apiCluster, spec)

		minReplicas := spec.Replicas
		maxReplicas := spec.Replicas
		if spec.MinReplicas != 0 {
			minReplicas = spec.MinReplicas
		}
		if spec.MaxReplicas != 0 {
			maxReplicas = spec.MaxReplicas
		}

		workerNodeSpec := rayclusterapi.WorkerGroupSpec{
			GroupName:      spec.GroupName,
			MinReplicas:    intPointer(minReplicas),
			MaxReplicas:    intPointer(maxReplicas),
			Replicas:       intPointer(spec.Replicas),
			RayStartParams: spec.RayStartParams,
			Template:       workerPodTemplate,
		}

		rayCluster.Spec.WorkerGroupSpecs = append(rayCluster.Spec.WorkerGroupSpecs, workerNodeSpec)
	}

	return &RayCluster{rayCluster}
}

func buildRayClusterLabels(cluster *api.Cluster, clusterRuntime *api.ClusterRuntime, computeRuntime *api.ComputeRuntime) map[string]string {
	labels := map[string]string{}
	labels[RayClusterNameLabelKey] = cluster.Name
	labels[RayClusterUserLabelKey] = cluster.User
	labels[RayClusterVersionLabelKey] = cluster.Version
	labels[RayClusterEnvironmentLabelKey] = cluster.Environment.String()
	labels[RayClusterComputeRuntimeTemplateLabelKey] = computeRuntime.Name
	labels[RayClusterClusterRuntimeTemplateLabelKey] = clusterRuntime.Name
	return labels
}

func buildRayClusterAnnotations(cluster *api.Cluster, clusterRuntime *api.ClusterRuntime, computeRuntime *api.ComputeRuntime) map[string]string {
	annotations := map[string]string{}
	annotations[RayClusterCloudAnnotationKey] = computeRuntime.Cloud.String()
	annotations[RayClusterRegionAnnotationKey] = computeRuntime.Region
	annotations[RayClusterAZAnnotationKey] = computeRuntime.AvailabilityZone

	annotations[RayClusterImageAnnotationKey] = clusterRuntime.Image
	return annotations
}

func buildHeadPodTemplate(cluster *api.Cluster) v1.PodTemplateSpec {
	containerImage := "rayproject/ray"
	version := cluster.Version
	// TODO: default resource for head. In the future, we can just let use pick size of the cluster and customize the cluster.
	cpu := "1"
	memory := "512Mi"

	podTemplateSpec := v1.PodTemplateSpec{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					// TODO: move to const later
					Name:  "ray-head",
					Image: constructRayImage(containerImage, version),
					// TODO: append env pairs from cluster runtime
					Env: []v1.EnvVar{
						{
							Name: "MY_POD_IP",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "status.podIP",
								},
							},
						},
					},
					Ports: []v1.ContainerPort{
						{
							Name:          "redis",
							ContainerPort: 6379,
						},
						{
							Name:          "head",
							ContainerPort: 10001,
						},
						{
							Name:          "dashboard",
							ContainerPort: 8265,
						},
					},
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(cpu),
							v1.ResourceMemory: resource.MustParse(memory),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(cpu),
							v1.ResourceMemory: resource.MustParse(memory),
						},
					},
				},
			},
		},
	}

	return podTemplateSpec
}

func constructRayImage(containerImage string, version string) string {
	return fmt.Sprintf("%s:%s", containerImage, version)
}

func buildWorkerPodTemplate(cluster *api.Cluster, spec *api.WorkerGroupSpec) v1.PodTemplateSpec {
	// TODO: verify the versions in the range
	containerImage := "rayproject/ray"
	version := cluster.Version
	cpu := "1"
	memory := "512Mi"
	podTemplateSpec := v1.PodTemplateSpec{
		Spec: v1.PodSpec{
			InitContainers: []v1.Container{
				{
					Name:  "init-myservice",
					Image: "busybox:1.28",
					Command: []string{
						"sh",
						"-c",
						"until nslookup $RAY_HEAD_SERVICE_IP.$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace).svc.cluster.local; do echo waiting for myservice; sleep 2; done",
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:  "ray-worker",
					Image: constructRayImage(containerImage, version),
					Env: []v1.EnvVar{
						{
							Name:  "RAY_DISABLE_DOCKER_CPU_WRARNING",
							Value: "1",
						},
						{
							Name:  "TYPE",
							Value: "worker",
						},
						{
							Name: "CPU_REQUEST",
							ValueFrom: &v1.EnvVarSource{
								ResourceFieldRef: &v1.ResourceFieldSelector{
									ContainerName: "ray-worker",
									Resource:      "requests.cpu",
								},
							},
						},
						{
							Name: "CPU_LIMITS",
							ValueFrom: &v1.EnvVarSource{
								ResourceFieldRef: &v1.ResourceFieldSelector{
									ContainerName: "ray-worker",
									Resource:      "limits.cpu",
								},
							},
						},
						{
							Name: "MEMORY_REQUESTS",
							ValueFrom: &v1.EnvVarSource{
								ResourceFieldRef: &v1.ResourceFieldSelector{
									ContainerName: "ray-worker",
									Resource:      "requests.cpu",
								},
							},
						},
						{
							Name: "MEMORY_LIMITS",
							ValueFrom: &v1.EnvVarSource{
								ResourceFieldRef: &v1.ResourceFieldSelector{
									ContainerName: "ray-worker",
									Resource:      "limits.cpu",
								},
							},
						},
						{
							Name: "MY_POD_NAME",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "metadata.name",
								},
							},
						},
						{
							Name: "MY_POD_IP",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "status.podIP",
								},
							},
						},
					},
					Ports: []v1.ContainerPort{
						{
							ContainerPort: 80,
						},
					},
					Lifecycle: &v1.Lifecycle{
						PreStop: &v1.Handler{
							Exec: &v1.ExecAction{
								Command: []string{
									"/bin/sh", "-c", "ray stop",
								},
							},
						},
					},
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(cpu),
							v1.ResourceMemory: resource.MustParse(memory),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(cpu),
							v1.ResourceMemory: resource.MustParse(memory),
						},
					},
				},
			},
		},
	}

	if spec.Resource.Gpu != 0 {
		// need smarter algorithm to filter main container. for example filter by name `ray-worker`
		podTemplateSpec.Spec.Containers[0].Resources.Requests["nvidia.com/gpu"] = resource.MustParse(fmt.Sprint(spec.Resource.Gpu))
		podTemplateSpec.Spec.Containers[0].Resources.Limits["nvidia.com/gpu"] = resource.MustParse(fmt.Sprint(spec.Resource.Gpu))
	}

	return podTemplateSpec
}

func intPointer(value int32) *int32 {
	return &value
}

// Get converts this object to a rayclusterapi.Workflow.
func (c *RayCluster) Get() *rayclusterapi.RayCluster {
	return c.RayCluster
}

// SetAnnotations sets annotations on all templates in a RayCluster
func (c *RayCluster) SetAnnotationsToAllTemplates(key string, value string) {

}

func NewClusterRuntime(runtime *api.ClusterRuntime, id, namespace string) (*v1.ConfigMap, error) {
	config := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id,
			Namespace: namespace,
			Labels: map[string]string{
				"ray.io/config-type": "cluster-runtime",
				"ray.io/cluster-runtime": runtime.Name,
			},
		},
		Data: map[string]string{
			"base_image":      runtime.BaseImage,
			"custom_commands": runtime.CustomCommands,
			"pip_packages":    strings.Join(runtime.PipPackages, ","),
			"conda_packages":  strings.Join(runtime.CondaPackages, ","),
			"system_packages": strings.Join(runtime.SystemPackages, ","),
			// TODO: We don't dynamically build image at this moment
			// we should trick job to build new images and use it here later.
			"image": runtime.BaseImage,
		},
	}

	if runtime.EnvironmentVariables != nil && len(runtime.EnvironmentVariables) != 0 {
		envs, err := json.Marshal(runtime.EnvironmentVariables)
		if err != nil {
			return nil, fmt.Errorf("can not covert protobuf to json %v", err)
		}
		config.Data["environment_variables"] = string(envs)
	} else {
		config.Data["environment_variables"] = "{}"
	}

	return config, nil
}

func NewComputeRuntime(runtime *api.ComputeRuntime, id, namespace string) (*v1.ConfigMap, error) {
	// https://www.linkedin.com/pulse/google-protocol-buffers-3-go-jos%C3%A9-augusto-zimmermann-negreiros
	// https://seb-nyberg.medium.com/customizing-protobuf-json-serialization-in-golang-6c58b5890356
	headSpecJson, err := protojson.Marshal(runtime.HeadGroupSpec)
	if err != nil {
		return nil, fmt.Errorf("can not covert protobuf to json %v", err)
	}

	// TODO: Marshal doesn't support list..
	// We concat strings manually here as a short term workaround
	// This should be revised later.
	specs := make([]string, 0)
	for _, workerNodeSpec := range runtime.WorkerGroupSepc {
		json, err := protojson.Marshal(workerNodeSpec)
		if err != nil {
			return nil, fmt.Errorf("can not covert protobuf to json %v", err)
		}
		fmt.Println(fmt.Sprintf("%v", string(json)))
		specs = append(specs, string(json))
	}

	config := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id,
			Namespace: namespace,
			Labels: map[string]string{
				"ray.io/config-type": "compute-runtime",
				"ray.io/compute-runtime": runtime.Name,
			},
		},
		Data: map[string]string{
			"name":              runtime.Name,
			"cloud":             runtime.Cloud.String(),
			"region":            runtime.Region,
			"availability_zone": runtime.AvailabilityZone,
			"head_node_spec":    string(headSpecJson),
			"worker_node_specs": "[" + strings.Join(specs, ",") + "]",
		},
	}

	return config, nil
}
