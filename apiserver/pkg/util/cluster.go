package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"

	klog "k8s.io/klog/v2"

	api "github.com/ray-project/kuberay/proto/go_client"
	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RayCluster struct {
	*rayv1api.RayCluster
}

// NewRayCluster creates a RayCluster.
// func NewRayCluster(apiCluster *api.Cluster, clusterRuntime *api.ClusterRuntime, computeRuntime *api.ComputeRuntime) *RayCluster {
func NewRayCluster(apiCluster *api.Cluster, computeTemplateMap map[string]*api.ComputeTemplate) (*RayCluster, error) {
	// Build cluster spec
	spec, err := buildRayClusterSpec(apiCluster.Version, apiCluster.Envs, apiCluster.ClusterSpec, computeTemplateMap)
	if err != nil {
		return nil, err
	}
	// Build cluster
	rayCluster := &rayv1api.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        apiCluster.Name,
			Namespace:   apiCluster.Namespace,
			Labels:      buildRayClusterLabels(apiCluster),
			Annotations: buildRayClusterAnnotations(apiCluster),
		},
		Spec: *spec,
	}

	return &RayCluster{rayCluster}, nil
}

// Build cluster labels
func buildRayClusterLabels(cluster *api.Cluster) map[string]string {
	labels := map[string]string{}
	labels[RayClusterNameLabelKey] = cluster.Name
	labels[RayClusterUserLabelKey] = cluster.User
	labels[RayClusterVersionLabelKey] = cluster.Version
	labels[RayClusterEnvironmentLabelKey] = cluster.Environment.String()
	labels[KubernetesApplicationNameLabelKey] = ApplicationName
	labels[KubernetesManagedByLabelKey] = ComponentName
	return labels
}

// Build cluster annotations
func buildRayClusterAnnotations(cluster *api.Cluster) map[string]string {
	if cluster.Annotations == nil {
		return map[string]string{}
	}
	return cluster.Annotations
}

// TODO(Basasuya & MissionToMars): The job spec depends on ClusterSpec which not all cluster-related configs are included,
// such as `metadata` and `envs`. We just put `imageVersion` and `envs` in the arguments list, and should be refactored later.
func buildRayClusterSpec(imageVersion string, envs *api.EnvironmentVariables, clusterSpec *api.ClusterSpec, computeTemplateMap map[string]*api.ComputeTemplate) (*rayv1api.RayClusterSpec, error) {
	computeTemplate := computeTemplateMap[clusterSpec.HeadGroupSpec.ComputeTemplate]
	headPodTemplate, err := buildHeadPodTemplate(imageVersion, envs, clusterSpec.HeadGroupSpec, computeTemplate)
	if err != nil {
		return nil, err
	}
	headReplicas := int32(1)
	rayClusterSpec := &rayv1api.RayClusterSpec{
		RayVersion: imageVersion,
		HeadGroupSpec: rayv1api.HeadGroupSpec{
			ServiceType:    v1.ServiceType(clusterSpec.HeadGroupSpec.ServiceType),
			Template:       *headPodTemplate,
			Replicas:       &headReplicas,
			RayStartParams: clusterSpec.HeadGroupSpec.RayStartParams,
		},
		WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{},
	}

	// If enable ingress is specified, add it to the head node spec.
	if clusterSpec.HeadGroupSpec.EnableIngress {
		rayClusterSpec.HeadGroupSpec.EnableIngress = &clusterSpec.HeadGroupSpec.EnableIngress
	}

	// Build worker groups
	for _, spec := range clusterSpec.WorkerGroupSpec {
		computeTemplate = computeTemplateMap[spec.ComputeTemplate]
		workerPodTemplate, err := buildWorkerPodTemplate(imageVersion, envs, spec, computeTemplate)
		if err != nil {
			return nil, err
		}

		minReplicas := spec.Replicas
		maxReplicas := spec.Replicas
		if spec.MinReplicas != 0 {
			minReplicas = spec.MinReplicas
		}
		if spec.MaxReplicas != 0 {
			maxReplicas = spec.MaxReplicas
		}

		workerNodeSpec := rayv1api.WorkerGroupSpec{
			GroupName:      spec.GroupName,
			MinReplicas:    intPointer(minReplicas),
			MaxReplicas:    intPointer(maxReplicas),
			Replicas:       intPointer(spec.Replicas),
			RayStartParams: spec.RayStartParams,
			Template:       *workerPodTemplate,
		}

		rayClusterSpec.WorkerGroupSpecs = append(rayClusterSpec.WorkerGroupSpecs, workerNodeSpec)
	}

	return rayClusterSpec, nil
}

// Annotations common to both head and worker nodes
func buildNodeGroupAnnotations(computeTemplate *api.ComputeTemplate, image string) map[string]string {
	annotations := map[string]string{}
	annotations[RayClusterComputeTemplateAnnotationKey] = computeTemplate.Name
	annotations[RayClusterImageAnnotationKey] = image
	return annotations
}

// Build head node template
func buildHeadPodTemplate(imageVersion string, envs *api.EnvironmentVariables, spec *api.HeadGroupSpec, computeRuntime *api.ComputeTemplate) (*v1.PodTemplateSpec, error) {
	image := constructRayImage(RayClusterDefaultImageRepository, imageVersion)
	if len(spec.Image) != 0 {
		image = spec.Image
	}

	// calculate resources
	cpu := fmt.Sprint(computeRuntime.GetCpu())
	memory := fmt.Sprintf("%d%s", computeRuntime.GetMemory(), "Gi")

	// build volume and volumeMounts
	volMounts := buildVolumeMounts(spec.Volumes)
	vols, err := buildVols(spec.Volumes)
	if err != nil {
		return nil, err
	}

	podTemplateSpec := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: buildNodeGroupAnnotations(computeRuntime, spec.Image),
			Labels:      map[string]string{},
		},
		Spec: v1.PodSpec{
			Tolerations: []v1.Toleration{},
			Containers: []v1.Container{
				{
					Name:  "ray-head",
					Image: image,
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
					// Customization is not allowed here. We should consider whether to make this part smart.
					// For now we use serve 8000 port for rayservice and added at util/service.go, do not use the 8000 port here for other purpose.
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
						{
							Name:          "metrics",
							ContainerPort: 8080,
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
					VolumeMounts: volMounts,
				},
			},
			Volumes: vols,
		},
	}

	// We are filtering container by name `ray-head`. If container with this name does not exist
	// (should never happen) we are not adding container specific parameters
	if container, index, ok := GetContainerByName(podTemplateSpec.Spec.Containers, "ray-head"); ok {
		if computeRuntime.GetGpu() != 0 {
			gpu := computeRuntime.GetGpu()
			accelerator := "nvidia.com/gpu"
			if len(computeRuntime.GetGpuAccelerator()) != 0 {
				accelerator = computeRuntime.GetGpuAccelerator()
			}
			container.Resources.Requests[v1.ResourceName(accelerator)] = resource.MustParse(fmt.Sprint(gpu))
			container.Resources.Limits[v1.ResourceName(accelerator)] = resource.MustParse(fmt.Sprint(gpu))
		}
		globalEnv := convertEnvironmentVariables(envs)
		if len(globalEnv) > 0 {
			container.Env = append(container.Env, globalEnv...)
		}

		// Add specific environments
		specEnv := convertEnvironmentVariables(spec.Environment)
		if len(specEnv) > 0 {
			container.Env = append(container.Env, specEnv...)
		}

		// Replace container
		podTemplateSpec.Spec.Containers[index] = container
	}

	// Add specific annotations
	if spec.Annotations != nil {
		for k, v := range spec.Annotations {
			podTemplateSpec.ObjectMeta.Annotations[k] = v
		}
	}

	// Add specific labels
	if spec.Labels != nil {
		for k, v := range spec.Labels {
			podTemplateSpec.ObjectMeta.Labels[k] = v
		}
	}

	// Add specific tollerations
	if computeRuntime.Tolerations != nil {
		for _, t := range computeRuntime.Tolerations {
			podTemplateSpec.Spec.Tolerations = append(podTemplateSpec.Spec.Tolerations, v1.Toleration{
				Key: t.Key, Operator: convertTolerationOperator(t.Operator), Value: t.Value, Effect: convertTaintEffect(t.Effect),
			})
		}
	}

	// If service account is specified, add it to the pod spec.
	if len(spec.ServiceAccount) > 1 {
		podTemplateSpec.Spec.ServiceAccountName = spec.ServiceAccount
	}

	// If image pull secret is specified, add it to the pod spec.
	if len(spec.ImagePullSecret) > 1 {
		podTemplateSpec.Spec.ImagePullSecrets = []v1.LocalObjectReference{
			{
				Name: spec.ImagePullSecret,
			},
		}
	}

	return &podTemplateSpec, nil
}

// Convert environment variables
func convertEnvironmentVariables(envs *api.EnvironmentVariables) []v1.EnvVar {
	converted := []v1.EnvVar{}
	if envs == nil {
		return converted
	}
	if envs.Values != nil && len(envs.Values) > 0 {
		// Add values
		for key, value := range envs.Values {
			converted = append(converted, v1.EnvVar{
				Name: key, Value: value,
			})
		}
	}
	if envs.ValuesFrom != nil && len(envs.ValuesFrom) > 0 {
		// Add values ref
		for key, value := range envs.ValuesFrom {
			switch value.Source {
			case api.EnvValueFrom_CONFIGMAP:
				converted = append(converted, v1.EnvVar{
					Name: key,
					ValueFrom: &v1.EnvVarSource{
						ConfigMapKeyRef: &v1.ConfigMapKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: value.Name,
							},
							Key: value.Key,
						},
					},
				})
			case api.EnvValueFrom_SECRET:
				converted = append(converted, v1.EnvVar{
					Name: key,
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: value.Name,
							},
							Key: value.Key,
						},
					},
				})
			case api.EnvValueFrom_RESOURCEFIELD:
				converted = append(converted, v1.EnvVar{
					Name: key,
					ValueFrom: &v1.EnvVarSource{
						ResourceFieldRef: &v1.ResourceFieldSelector{
							ContainerName: value.Name,
							Resource:      value.Key,
						},
					},
				})
			default:
				converted = append(converted, v1.EnvVar{
					Name: key,
					ValueFrom: &v1.EnvVarSource{
						FieldRef: &v1.ObjectFieldSelector{
							FieldPath: value.Key,
						},
					},
				})
			}
		}
	}
	return converted
}

// Convert Toleration operator from string
func convertTolerationOperator(val string) v1.TolerationOperator {
	if val == "Exists" {
		return v1.TolerationOpExists
	}
	return v1.TolerationOpEqual
}

// Convert taint effect from string
func convertTaintEffect(val string) v1.TaintEffect {
	if val == "NoExecute" {
		return v1.TaintEffectNoExecute
	}
	if val == "NoSchedule" {
		return v1.TaintEffectNoSchedule
	}
	return v1.TaintEffectPreferNoSchedule
}

// Construct Ray image
func constructRayImage(containerImage string, version string) string {
	return fmt.Sprintf("%s:%s", containerImage, version)
}

// Build worker pod template
func buildWorkerPodTemplate(imageVersion string, envs *api.EnvironmentVariables, spec *api.WorkerGroupSpec, computeRuntime *api.ComputeTemplate) (*v1.PodTemplateSpec, error) {
	// If user doesn't provide the image, let's use the default image instead.
	// TODO: verify the versions in the range
	image := constructRayImage(RayClusterDefaultImageRepository, imageVersion)
	if len(spec.Image) != 0 {
		image = spec.Image
	}

	// calculate resources
	cpu := fmt.Sprint(computeRuntime.GetCpu())
	memory := fmt.Sprintf("%d%s", computeRuntime.GetMemory(), "Gi")

	// build volume and volumeMounts
	volMounts := buildVolumeMounts(spec.Volumes)
	vols, err := buildVols(spec.Volumes)
	if err != nil {
		return nil, err
	}

	podTemplateSpec := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: buildNodeGroupAnnotations(computeRuntime, spec.Image),
			Labels:      map[string]string{},
		},
		Spec: v1.PodSpec{
			Tolerations: []v1.Toleration{},
			Containers: []v1.Container{
				{
					Name:  "ray-worker",
					Image: image,
					Env: []v1.EnvVar{
						{
							Name:  "RAY_DISABLE_DOCKER_CPU_WARNING",
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
						PreStop: &v1.LifecycleHandler{
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
					VolumeMounts: volMounts,
				},
			},
			Volumes: vols,
		},
	}

	// We are filtering container by name `ray-worker`. If container with this name does not exist
	// (should never happen) we are not adding container specific parameters
	if container, index, ok := GetContainerByName(podTemplateSpec.Spec.Containers, "ray-worker"); ok {
		if computeRuntime.GetGpu() != 0 {
			gpu := computeRuntime.GetGpu()
			accelerator := "nvidia.com/gpu"
			if len(computeRuntime.GetGpuAccelerator()) != 0 {
				accelerator = computeRuntime.GetGpuAccelerator()
			}

			// need smarter algorithm to filter main container. for example filter by name `ray-worker`
			container.Resources.Requests[v1.ResourceName(accelerator)] = resource.MustParse(fmt.Sprint(gpu))
			container.Resources.Limits[v1.ResourceName(accelerator)] = resource.MustParse(fmt.Sprint(gpu))
		}

		globalEnv := convertEnvironmentVariables(envs)
		if len(globalEnv) > 0 {
			container.Env = append(container.Env, globalEnv...)
		}

		// Add specific environments
		specEnv := convertEnvironmentVariables(spec.Environment)
		if len(specEnv) > 0 {
			container.Env = append(container.Env, specEnv...)
		}

		// Replace container
		podTemplateSpec.Spec.Containers[index] = container
	}

	// Add specific annotations
	if spec.Annotations != nil {
		for k, v := range spec.Annotations {
			podTemplateSpec.ObjectMeta.Annotations[k] = v
		}
	}

	// Add specific labels
	if spec.Labels != nil {
		for k, v := range spec.Labels {
			podTemplateSpec.ObjectMeta.Labels[k] = v
		}
	}

	// Add specific tollerations
	if computeRuntime.Tolerations != nil {
		for _, t := range computeRuntime.Tolerations {
			podTemplateSpec.Spec.Tolerations = append(podTemplateSpec.Spec.Tolerations, v1.Toleration{
				Key: t.Key, Operator: convertTolerationOperator(t.Operator), Value: t.Value, Effect: convertTaintEffect(t.Effect),
			})
		}
	}

	// If service account is specified, add it to the pod spec.
	if len(spec.ServiceAccount) > 1 {
		podTemplateSpec.Spec.ServiceAccountName = spec.ServiceAccount
	}

	// If image pull secret is specified, add it to the pod spec.
	if len(spec.ImagePullSecret) > 1 {
		podTemplateSpec.Spec.ImagePullSecrets = []v1.LocalObjectReference{
			{
				Name: spec.ImagePullSecret,
			},
		}
	}

	return &podTemplateSpec, nil
}

// Build Volume mounts
func buildVolumeMounts(apiVolumes []*api.Volume) []v1.VolumeMount {
	var (
		volMounts       []v1.VolumeMount
		hostToContainer = v1.MountPropagationHostToContainer
		bidirectonal    = v1.MountPropagationBidirectional
	)
	for _, vol := range apiVolumes {
		volMount := v1.VolumeMount{
			Name:      vol.Name,
			ReadOnly:  vol.ReadOnly,
			MountPath: vol.MountPath,
		}
		switch vol.MountPropagationMode {
		case api.Volume_HOSTTOCONTAINER:
			volMount.MountPropagation = &hostToContainer
		case api.Volume_BIDIRECTIONAL:
			volMount.MountPropagation = &bidirectonal
		}
		volMounts = append(volMounts, volMount)
	}
	return volMounts
}

// Build host path
func newHostPathType(pathType string) *v1.HostPathType {
	hostPathType := new(v1.HostPathType)
	*hostPathType = v1.HostPathType(pathType)
	return hostPathType
}

// Build volumes
func buildVols(apiVolumes []*api.Volume) ([]v1.Volume, error) {
	var vols []v1.Volume
	for _, rayVol := range apiVolumes {
		if rayVol.VolumeType == api.Volume_CONFIGMAP {
			vol := v1.Volume{
				Name: rayVol.Name,
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: rayVol.Source,
						},
					},
				},
			}
			if len(rayVol.Items) > 0 {
				// Add items
				items := []v1.KeyToPath{}
				for key, value := range rayVol.Items {
					items = append(items, v1.KeyToPath{Key: key, Path: value})
				}
				vol.ConfigMap.Items = items
			}
			vols = append(vols, vol)
		}
		if rayVol.VolumeType == api.Volume_SECRET {
			vol := v1.Volume{
				Name: rayVol.Name,
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: rayVol.Source,
					},
				},
			}
			if len(rayVol.Items) > 0 {
				// Add items
				items := []v1.KeyToPath{}
				for key, value := range rayVol.Items {
					items = append(items, v1.KeyToPath{Key: key, Path: value})
				}
				vol.Secret.Items = items
			}
			vols = append(vols, vol)
		}
		if rayVol.VolumeType == api.Volume_EMPTY_DIR {
			vol := v1.Volume{
				Name: rayVol.Name,
				VolumeSource: v1.VolumeSource{
					EmptyDir: &v1.EmptyDirVolumeSource{},
				},
			}
			if rayVol.Storage != "" {
				// Max Storage size is  defined
				// Ensure that storage size is formatted correctly
				_, err := resource.ParseQuantity(rayVol.Storage)
				if err != nil {
					return nil, errors.New("storage for empty dir volume is not specified correctly")
				}
				limit := resource.MustParse(rayVol.Storage)
				vol.EmptyDir.SizeLimit = &limit
			}
			vols = append(vols, vol)
		}
		if rayVol.VolumeType == api.Volume_HOST_PATH {
			vol := v1.Volume{
				Name: rayVol.Name,
				VolumeSource: v1.VolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: rayVol.Source,
					},
				},
			}
			switch rayVol.HostPathType {
			case api.Volume_DIRECTORY:
				vol.VolumeSource.HostPath.Type = newHostPathType(string(v1.HostPathDirectory))
			case api.Volume_FILE:
				vol.VolumeSource.HostPath.Type = newHostPathType(string(v1.HostPathFile))
			default:
				vol.VolumeSource.HostPath.Type = newHostPathType(string(v1.HostPathDirectory))
			}
			vols = append(vols, vol)
		}
		if rayVol.VolumeType == api.Volume_PERSISTENT_VOLUME_CLAIM {
			vol := v1.Volume{
				Name: rayVol.Name,
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: rayVol.Name,
						ReadOnly:  rayVol.ReadOnly,
					},
				},
			}
			vols = append(vols, vol)
		}
		if rayVol.VolumeType == api.Volume_EPHEMERAL {
			// Make sure that at least the storage size is defined
			if rayVol.Storage == "" {
				// Storage size is not defined
				return nil, errors.New("storage for ephemeral volume is empty")
			}
			// Ensure that storage size is formatted correctly
			_, err := resource.ParseQuantity(rayVol.Storage)
			if err != nil {
				return nil, errors.New("storage for ephemeral volume is not specified correctly")
			}
			vol := v1.Volume{
				Name: rayVol.Name,
				VolumeSource: v1.VolumeSource{
					Ephemeral: &v1.EphemeralVolumeSource{
						VolumeClaimTemplate: &v1.PersistentVolumeClaimTemplate{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app.kubernetes.io/managed-by": "kuberay-apiserver",
								},
							},
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceStorage: resource.MustParse(rayVol.Storage),
									},
								},
							},
						},
					},
				},
			}
			if len(rayVol.StorageClassName) > 0 {
				// Populate storage class, if defined
				vol.VolumeSource.Ephemeral.VolumeClaimTemplate.Spec.StorageClassName = &rayVol.StorageClassName
			}

			// Populate access mode if defined
			switch rayVol.AccessMode {
			case api.Volume_RWO:
				vol.VolumeSource.Ephemeral.VolumeClaimTemplate.Spec.AccessModes = []v1.PersistentVolumeAccessMode{
					v1.ReadWriteOnce,
				}
			case api.Volume_RWX:
				vol.VolumeSource.Ephemeral.VolumeClaimTemplate.Spec.AccessModes = []v1.PersistentVolumeAccessMode{
					v1.ReadWriteMany,
				}
			case api.Volume_ROX:
				vol.VolumeSource.Ephemeral.VolumeClaimTemplate.Spec.AccessModes = []v1.PersistentVolumeAccessMode{
					v1.ReadOnlyMany,
				}
			default:
				vol.VolumeSource.Ephemeral.VolumeClaimTemplate.Spec.AccessModes = []v1.PersistentVolumeAccessMode{
					v1.ReadWriteOnce,
				}
			}
			vols = append(vols, vol)
		}
	}

	return vols, nil
}

// Init pointer
func intPointer(value int32) *int32 {
	return &value
}

// Get converts this object to a rayv1api.Workflow.
func (c *RayCluster) Get() *rayv1api.RayCluster {
	return c.RayCluster
}

// SetAnnotations sets annotations on all templates in a RayCluster
func (c *RayCluster) SetAnnotationsToAllTemplates(key string, value string) {
	// TODO: reserved for common parameters.
}

// Build compute template
func NewComputeTemplate(runtime *api.ComputeTemplate) (*v1.ConfigMap, error) {
	// Create data map
	dmap := map[string]string{
		"name":            runtime.Name,
		"namespace":       runtime.Namespace,
		"cpu":             strconv.FormatUint(uint64(runtime.Cpu), 10),
		"memory":          strconv.FormatUint(uint64(runtime.Memory), 10),
		"gpu":             strconv.FormatUint(uint64(runtime.Gpu), 10),
		"gpu_accelerator": runtime.GpuAccelerator,
	}
	// Add tolerations in defined
	if runtime.Tolerations != nil && len(runtime.Tolerations) > 0 {
		t, err := json.Marshal(runtime.Tolerations)
		if err != nil {
			klog.Errorf("failed to marshall tolerations ", runtime.Tolerations, " for compute template ", runtime.Name,
				" error ", err)
		} else {
			dmap["tolerations"] = string(t)
		}
	}

	config := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runtime.Name,
			Namespace: runtime.Namespace,
			Labels: map[string]string{
				"ray.io/config-type":      "compute-template",
				"ray.io/compute-template": runtime.Name,
			},
		},
		Data: dmap,
	}

	return config, nil
}

// GetNodeHostIP returns the provided node's IP, based on the priority:
// 1. NodeInternalIP
// 2. NodeExternalIP
func GetNodeHostIP(node *v1.Node) (net.IP, error) {
	addresses := node.Status.Addresses
	addressMap := make(map[v1.NodeAddressType][]v1.NodeAddress)
	for _, nodeAddress := range addresses {
		addressMap[nodeAddress.Type] = append(addressMap[nodeAddress.Type], nodeAddress)
	}
	if addresses, ok := addressMap[v1.NodeInternalIP]; ok {
		return net.ParseIP(addresses[0].Address), nil
	}
	if addresses, ok := addressMap[v1.NodeExternalIP]; ok {
		return net.ParseIP(addresses[0].Address), nil
	}
	return nil, fmt.Errorf("host IP unknown; known addresses: %v", addresses)
}

func GetContainerByName(containers []v1.Container, name string) (v1.Container, int, bool) {
	for index, container := range containers {
		if container.Name == name {
			return container, index, true
		}
	}
	return v1.Container{}, 0, false
}
