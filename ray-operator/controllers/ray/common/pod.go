package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SharedMemoryVolumeName      = "shared-mem"
	SharedMemoryVolumeMountPath = "/dev/shm"
	RayLogVolumeName            = "ray-logs"
	RayLogVolumeMountPath       = "/tmp/ray"
	AutoscalerContainerName     = "autoscaler"
)

var log = logf.Log.WithName("RayCluster-Controller")

// DefaultHeadPodTemplate sets the config values
func DefaultHeadPodTemplate(instance rayiov1alpha1.RayCluster, headSpec rayiov1alpha1.HeadGroupSpec, podName string, svcName string) v1.PodTemplateSpec {
	podTemplate := headSpec.Template
	podTemplate.GenerateName = podName
	if podTemplate.ObjectMeta.Namespace == "" {
		podTemplate.ObjectMeta.Namespace = instance.Namespace
		log.Info("Setting pod namespaces", "namespace", instance.Namespace)
	}

	if podTemplate.Labels == nil {
		podTemplate.Labels = make(map[string]string)
	}
	podTemplate.Labels = labelPod(rayiov1alpha1.HeadNode, instance.Name, "headgroup", instance.Spec.HeadGroupSpec.Template.ObjectMeta.Labels)
	headSpec.RayStartParams = setMissingRayStartParams(headSpec.RayStartParams, rayiov1alpha1.HeadNode, svcName)

	// if in-tree autoscaling is enabled, then autoscaler container should be injected into head pod.
	if instance.Spec.EnableInTreeAutoscaling != nil && *instance.Spec.EnableInTreeAutoscaling {
		headSpec.RayStartParams["no-monitor"] = "true"
		// set custom service account with proper roles bound.
		podTemplate.Spec.ServiceAccountName = utils.GetHeadGroupServiceAccountName(&instance)

		// inject autoscaler container into head pod
		autoscalerContainer := BuildAutoscalerContainer()
		// Merge the user overrides from autoscalerOptions into the autoscaler container config.
		mergeAutoscalerOverrides(&autoscalerContainer, instance.Spec.AutoscalerOptions)
		podTemplate.Spec.Containers = append(podTemplate.Spec.Containers, autoscalerContainer)
	}

	// add metrics port for exposing to the promethues stack.
	metricsPort := v1.ContainerPort{
		Name:          "metrics",
		ContainerPort: int32(DefaultMetricsPort),
	}
	podTemplate.Spec.Containers[0].Ports = append(podTemplate.Spec.Containers[0].Ports, metricsPort)

	return podTemplate
}

// DefaultWorkerPodTemplate sets the config values
func DefaultWorkerPodTemplate(instance rayiov1alpha1.RayCluster, workerSpec rayiov1alpha1.WorkerGroupSpec, podName string, svcName string) v1.PodTemplateSpec {
	podTemplate := workerSpec.Template
	podTemplate.GenerateName = podName
	if podTemplate.ObjectMeta.Namespace == "" {
		podTemplate.ObjectMeta.Namespace = instance.Namespace
		log.Info("Setting pod namespaces", "namespace", instance.Namespace)
	}

	if podTemplate.Labels == nil {
		podTemplate.Labels = make(map[string]string)
	}
	podTemplate.Labels = labelPod(rayiov1alpha1.WorkerNode, instance.Name, workerSpec.GroupName, workerSpec.Template.ObjectMeta.Labels)
	workerSpec.RayStartParams = setMissingRayStartParams(workerSpec.RayStartParams, rayiov1alpha1.WorkerNode, svcName)

	// add metrics port for exposing to the promethues stack.
	metricsPort := v1.ContainerPort{
		Name:          "metrics",
		ContainerPort: int32(DefaultMetricsPort),
	}
	dupIndex := -1
	for i, port := range podTemplate.Spec.Containers[0].Ports {
		if port.Name == metricsPort.Name {
			dupIndex = i
			break
		}
	}
	if dupIndex < 0 {
		podTemplate.Spec.Containers[0].Ports = append(podTemplate.Spec.Containers[0].Ports, metricsPort)
	} else {
		podTemplate.Spec.Containers[0].Ports[dupIndex] = metricsPort
	}

	return podTemplate
}

// The map `detectedRayResources` is used internally in this method as part of the Ray start entrypoint.
// This map is also returned so that it can be placed in RayCluster.status later in the reconcile iteration.
func BuildRayResources(podTemplateSpec v1.PodTemplateSpec, rayStartParams map[string]string, rayResourceSpec rayiov1alpha1.RayResources) (detectedRayResources rayiov1alpha1.RayResources) {
	rayContainerIndex := getRayContainerIndex(podTemplateSpec.Spec)
	rayContainerResources := podTemplateSpec.Spec.Containers[rayContainerIndex].Resources

	// Update user-provided rayResource spec with data from rayStartParams and the
	// pod spec.
	detectedRayResources = utils.ComputeRayResources(
		rayResourceSpec,
		rayStartParams,
		rayContainerResources,
	)
	return detectedRayResources
}

// BuildPod builds a pod config.
// The returned `Pod` will be used to create Ray pods.
func BuildPod(podTemplateSpec v1.PodTemplateSpec, rayNodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string, svcName string, enableRayAutoscaler *bool, detectedRayResources rayiov1alpha1.RayResources) (aPod v1.Pod) {

	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: podTemplateSpec.ObjectMeta,
		Spec:       podTemplateSpec.Spec,
	}
	rayContainerIndex := getRayContainerIndex(pod.Spec)
	setRayContainerResourceEnvVar(&pod.Spec.Containers[rayContainerIndex], detectedRayResources)

	// Add /dev/shm volumeMount for the object store to avoid performance degradation.
	addEmptyDir(&pod.Spec.Containers[rayContainerIndex], &pod, SharedMemoryVolumeName, SharedMemoryVolumeMountPath, v1.StorageMediumMemory)
	if rayNodeType == rayiov1alpha1.HeadNode && enableRayAutoscaler != nil && *enableRayAutoscaler {
		// The Ray autoscaler writes logs which are read by the Ray head.
		// We need a shared log volume to enable this information flow.
		// Specifically, this is required for the event-logging functionality
		// introduced in https://github.com/ray-project/ray/pull/13434.
		autoscalerContainerIndex := getAutoscalerContainerIndex(podTemplateSpec.Spec)
		addEmptyDir(&pod.Spec.Containers[rayContainerIndex], &pod, RayLogVolumeName, RayLogVolumeMountPath, v1.StorageMediumDefault)
		addEmptyDir(&pod.Spec.Containers[autoscalerContainerIndex], &pod, RayLogVolumeName, RayLogVolumeMountPath, v1.StorageMediumDefault)
	}
	cleanupInvalidVolumeMounts(&pod.Spec.Containers[rayContainerIndex], &pod)
	if len(pod.Spec.InitContainers) > rayContainerIndex {
		cleanupInvalidVolumeMounts(&pod.Spec.InitContainers[rayContainerIndex], &pod)
	}

	var cmd, args string
	if len(pod.Spec.Containers[rayContainerIndex].Command) > 0 {
		cmd = convertCmdToString(pod.Spec.Containers[rayContainerIndex].Command)
	}
	if len(pod.Spec.Containers[rayContainerIndex].Args) > 0 {
		cmd += convertCmdToString(pod.Spec.Containers[rayContainerIndex].Args)
	}
	if !strings.Contains(cmd, "ray start") {
		cont := concatenateContainerCommand(rayNodeType, rayStartParams)
		// replacing the old command
		pod.Spec.Containers[rayContainerIndex].Command = []string{"/bin/bash", "-c", "--"}
		if cmd != "" {
			args = fmt.Sprintf("%s && %s", cont, cmd)
		} else {
			args = cont
		}

		// TODO (Dmitri) Always append block?
		if !isRayStartWithBlock(rayStartParams) {
			// sleep infinity is used to keep the pod `running` after the last command exits, and not go into `completed` state

			// TODO (Dmitri) TRAP interrupt and terminate signals for faster pod termination?
			args = args + " && sleep infinity"
		}

		pod.Spec.Containers[rayContainerIndex].Args = []string{args}
	}

	for index := range pod.Spec.InitContainers {
		setInitContainerEnvVars(&pod.Spec.InitContainers[index], svcName)
	}

	setRayContainerEnvVars(&pod.Spec.Containers[rayContainerIndex], rayNodeType, rayStartParams, svcName)

	return pod
}

// BuildAutoscalerContainer builds a Ray autoscaler container which can be appended to the head pod.
func BuildAutoscalerContainer() v1.Container {
	container := v1.Container{
		Name: AutoscalerContainerName,
		// TODO: choose right version based on instance.spec.Version
		// The currently used image reflects changes up to https://github.com/ray-project/ray/pull/24718
		Image:           "rayproject/ray:448f52",
		ImagePullPolicy: v1.PullAlways,
		Env: []v1.EnvVar{
			{
				Name: "RAY_CLUSTER_NAME",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.labels['ray.io/cluster']",
					},
				},
			},
			{
				Name: "RAY_CLUSTER_NAMESPACE",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
		},
		Command: []string{
			"ray",
		},
		Args: []string{
			"kuberay-autoscaler",
			"--cluster-name",
			"$(RAY_CLUSTER_NAME)",
			"--cluster-namespace",
			"$(RAY_CLUSTER_NAMESPACE)",
		},
		// TODO: make resource requirement configurable.
		Resources: v1.ResourceRequirements{
			Limits: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("500m"),
				v1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("256m"),
				v1.ResourceMemory: resource.MustParse("256Mi"),
			},
		},
	}
	return container
}

// Merge the user overrides from autoscalerOptions into the autoscaler container config.
func mergeAutoscalerOverrides(autoscalerContainer *v1.Container, autoscalerOptions *rayiov1alpha1.AutoscalerOptions) {
	if autoscalerOptions != nil {
		if autoscalerOptions.Resources != nil {
			autoscalerContainer.Resources = *autoscalerOptions.Resources
		}
		if autoscalerOptions.Image != nil {
			autoscalerContainer.Image = *autoscalerOptions.Image
		}
		if autoscalerOptions.ImagePullPolicy != nil {
			autoscalerContainer.ImagePullPolicy = *autoscalerOptions.ImagePullPolicy
		}
	}
}

func isRayStartWithBlock(rayStartParams map[string]string) bool {
	if blockValue, exist := rayStartParams["block"]; exist {
		return strings.ToLower(blockValue) == "true"
	}
	return false
}

func convertCmdToString(cmdArr []string) (cmd string) {
	cmdAggr := new(bytes.Buffer)
	for _, v := range cmdArr {
		fmt.Fprintf(cmdAggr, " %s ", v)
	}
	return cmdAggr.String()
}

func getRayContainerIndex(podSpec v1.PodSpec) (rayContainerIndex int) {
	// a ray pod can have multiple containers.
	// we identify the ray container based on env var: RAY=true
	// if the env var is missing, we choose containers[0].
	for i, container := range podSpec.Containers {
		for _, env := range container.Env {
			if env.Name == strings.ToLower("ray") && env.Value == strings.ToLower("true") {
				log.Info("Head pod container with index " + strconv.Itoa(i) + " identified as Ray container based on env RAY=true.")
				return i
			}
		}
	}
	// not found, use first container
	log.Info("Head pod container with index 0 identified as Ray container.")
	return 0
}

func getAutoscalerContainerIndex(podSpec v1.PodSpec) (autoscalerContainerIndex int) {
	// we identify the autoscaler container based on its name
	for i, container := range podSpec.Containers {
		if container.Name == AutoscalerContainerName {
			return i
		}
	}

	// This should be unreachable.
	panic("Autoscaler container not found!")
}

// labelPod returns the labels for selecting the resources
// belonging to the given RayCluster CR name.
func labelPod(rayNodeType rayiov1alpha1.RayNodeType, rayClusterName string, groupName string, labels map[string]string) (ret map[string]string) {
	if labels == nil {
		labels = make(map[string]string)
	}

	ret = map[string]string{
		RayNodeLabelKey:      "yes",
		RayClusterLabelKey:   rayClusterName,
		RayNodeTypeLabelKey:  string(rayNodeType),
		RayNodeGroupLabelKey: groupName,
		RayIDLabelKey:        utils.CheckLabel(utils.GenerateIdentifier(rayClusterName, rayNodeType)),
	}

	for k, v := range ret {
		if k == string(rayNodeType) {
			// overriding invalid values for this label
			if v != string(rayiov1alpha1.HeadNode) && v != string(rayiov1alpha1.WorkerNode) {
				labels[k] = v
			}
		}
		if k == RayNodeGroupLabelKey {
			// overriding invalide values for this label
			if v != groupName {
				labels[k] = v
			}
		}
		if _, ok := labels[k]; !ok {
			labels[k] = v
		}
	}
	return labels
}

func setContainerEnvVar(container *v1.Container, key string, value string) {
	// RAY_IP can be used in the DNS lookup
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []v1.EnvVar{}
	}
	if !envVarExists(key, container.Env) {
		envVar := v1.EnvVar{Name: key}
		envVar.Value = value
		container.Env = append(container.Env, envVar)
	}
}

func setInitContainerEnvVars(initContainer *v1.Container, svcName string) {
	// RAY_IP can be used in the DNS lookup
	setContainerEnvVar(initContainer, RAY_IP, svcName)
}

func setRayContainerResourceEnvVar(rayContainer *v1.Container, detectedRayResources rayiov1alpha1.RayResources) {
	// RAY_IP can be used in the DNS lookup
	resourceJSON, err := json.Marshal(detectedRayResources)
	if err != nil {
		// TODO (Dmitri) Pass error up the call stack.
		log.Error(err, "Failed to parse Ray resources.")
		return
	}
	quotedResourceJSON := shellescape.Quote(string(resourceJSON))
	setContainerEnvVar(rayContainer, RAY_OVERRIDE_RESOURCES, quotedResourceJSON)
}

func setRayContainerEnvVars(rayContainer *v1.Container, rayNodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string, svcName string) {
	// set IP to local host if head, or the the svc otherwise  RAY_IP
	// set the port RAY_PORT
	// set the password?
	var rayIpValue string
	if rayNodeType == rayiov1alpha1.HeadNode {
		// if head, use localhost
		rayIpValue = "127.0.0.1"
	} else {
		// if worker, use the service name of the head
		rayIpValue = svcName
	}
	setContainerEnvVar(rayContainer, RAY_IP, rayIpValue)

	var portValue string
	if value, ok := rayStartParams["port"]; !ok {
		// using default port
		portValue = strconv.Itoa(DefaultRedisPort)
	} else {
		// setting the RAY_PORT env var from the params
		portValue = value
	}
	setContainerEnvVar(rayContainer, RAY_PORT, portValue)

	if value, ok := rayStartParams["redis-password"]; ok {
		setContainerEnvVar(rayContainer, REDIS_PASSWORD, value)
	}
}

func envVarExists(envName string, envVars []v1.EnvVar) bool {
	if len(envVars) == 0 {
		return false
	}

	for _, env := range envVars {
		if env.Name == envName {
			return true
		}
	}
	return false
}

// TODO auto complete params
func setMissingRayStartParams(rayStartParams map[string]string, nodeType rayiov1alpha1.RayNodeType, svcName string) (completeStartParams map[string]string) {
	if nodeType == rayiov1alpha1.WorkerNode {
		if _, ok := rayStartParams["address"]; !ok {
			address := svcName
			if _, okPort := rayStartParams["port"]; !okPort {
				address = fmt.Sprintf("%s:%s", address, "6379")
			} else {
				address = fmt.Sprintf("%s:%s", address, rayStartParams["port"])
			}
			rayStartParams["address"] = address
		}
	}

	// add metrics port for expose the metrics to the prometheus.
	if _, ok := rayStartParams["metrics-export-port"]; !ok {
		rayStartParams["metrics-export-port"] = fmt.Sprint(DefaultMetricsPort)
	}

	return rayStartParams
}

// concatenateContainerCommand with ray start
func concatenateContainerCommand(nodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string) (fullCmd string) {

	log.V(10).Info("concatenate container command", "ray start params", rayStartParams)

	switch nodeType {
	case rayiov1alpha1.HeadNode:
		return fmt.Sprintf("ulimit -n 65536; ray start --head %s", convertParamMap(rayStartParams))
	case rayiov1alpha1.WorkerNode:
		return fmt.Sprintf("ulimit -n 65536; ray start %s", convertParamMap(rayStartParams))
	default:
		// TODO (Dmitri) Panic? Pass an error up the call stack?
		log.Error(fmt.Errorf("missing node type"), "a node must be either head or worker")
		return ""
	}

}

func convertParamMap(rayStartParams map[string]string) (s string) {
	flags := new(bytes.Buffer)
	for k, v := range rayStartParams {
		if strings.ToLower(v) == "true" {
			fmt.Fprintf(flags, " --%s ", k)
		} else {
			fmt.Fprintf(flags, " --%s=%s ", k, v)
		}
	}
	return flags.String()
}

// addEmptyDir adds an emptyDir volume to the pod and a corresponding volume mount to the container
// Used for a /dev/shm memory mount for object store and for a /tmp/ray disk mount for autoscaler logs.
func addEmptyDir(container *v1.Container, pod *v1.Pod, volumeName string, volumeMountPath string, storageMedium v1.StorageMedium) {
	if checkIfVolumeMounted(container, pod, volumeMountPath) {
		return
	}
	// 1) If needed, create a Volume of type emptyDir and add it to Volumes.
	if !checkIfVolumeExists(pod, volumeName) {
		emptyDirVolume := makeEmptyDirVolume(container, volumeName, storageMedium)
		pod.Spec.Volumes = append(pod.Spec.Volumes, emptyDirVolume)
	}

	// 2) Create a VolumeMount that uses the emptyDir.
	mountedVolume := v1.VolumeMount{
		MountPath: volumeMountPath,
		Name:      volumeName,
		ReadOnly:  false,
	}
	container.VolumeMounts = append(container.VolumeMounts, mountedVolume)
}

// Format an emptyDir volume.
// When the storage medium is memory, set the size limit based on container resources.
// For other media, don't set a size limit.
func makeEmptyDirVolume(container *v1.Container, volumeName string, storageMedium v1.StorageMedium) v1.Volume {
	var sizeLimit *resource.Quantity
	if storageMedium == v1.StorageMediumMemory {
		// If using memory, set size limit based on primary container's resources.
		sizeLimit = findMemoryReqOrLimit(*container)
	} else {
		// Otherwise, don't set a limit.
		sizeLimit = nil
	}
	return v1.Volume{
		Name: volumeName,
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium:    storageMedium,
				SizeLimit: sizeLimit,
			},
		},
	}
}

// Checks if the container has a volumeMount with the given mount path and if
// the pod has a matching Volume.
func checkIfVolumeMounted(container *v1.Container, pod *v1.Pod, volumeMountPath string) bool {
	for _, mountedVol := range container.VolumeMounts {
		if mountedVol.MountPath == volumeMountPath {
			for _, podVolume := range pod.Spec.Volumes {
				if mountedVol.Name == podVolume.Name {
					// already mounted, nothing to do
					return true
				}
			}
		}
	}
	return false
}

// Checks if a volume with the given name exists.
func checkIfVolumeExists(pod *v1.Pod, volumeName string) bool {
	for _, podVolume := range pod.Spec.Volumes {
		if podVolume.Name == volumeName {
			return true
		}
	}
	return false
}

func cleanupInvalidVolumeMounts(container *v1.Container, pod *v1.Pod) {
	// if a volumeMount is specified in the container,
	// but has no corresponding pod volume, it is removed
	for index, mountedVol := range container.VolumeMounts {
		valid := false
		for _, podVolume := range pod.Spec.Volumes {
			if mountedVol.Name == podVolume.Name {
				// valid mount, moving on...
				valid = true
				break
			}
		}
		if !valid {
			// remove the VolumeMount
			container.VolumeMounts[index] = container.VolumeMounts[len(container.VolumeMounts)-1]
			container.VolumeMounts = container.VolumeMounts[:len(container.VolumeMounts)-1]
		}
	}
}

func findMemoryReqOrLimit(container v1.Container) (res *resource.Quantity) {
	var mem *resource.Quantity
	// check the requests, if they are not set, check the limits.
	if q, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
		mem = &q
		return mem
	}
	if q, ok := container.Resources.Limits[v1.ResourceMemory]; ok {
		mem = &q
		return mem
	}
	return nil
}

// Stores Pod config data derived from the RayCluster CR.
// This type exists solely to tidy the return values of RayClusterReconciler.buildPodConfig
type RayPodConfig struct {
	// Configuration for the Ray head pod.
	HeadPod v1.Pod
	// Configuration for Ray worker pods. One entry per worker group.
	WorkerPods []v1.Pod
	// Resource capacity (CPU, GPU, memory, custom resorces) of the Ray head.
	HeadRayResources rayiov1alpha1.RayResources
	// Resource capacity (CPU, GPU, memory, custom resorces) of the Ray workers.
	WorkerRayResources []rayiov1alpha1.RayResources
}
