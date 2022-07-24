package common

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SharedMemoryVolumeName      = "shared-mem"
	SharedMemoryVolumeMountPath = "/dev/shm"
	RayLogVolumeName            = "ray-logs"
	RayLogVolumeMountPath       = "/tmp/ray/session_latest/logs"
	AutoscalerContainerName     = "autoscaler"
	RayHeadContainer            = "ray-head"
	ObjectStoreMemoryKey        = "object-store-memory"
	// TODO (davidxia): should be a const in upstream ray-project/ray
	AllowSlowStorageEnvVar = "RAY_OBJECT_STORE_ALLOW_SLOW_STORAGE"
)

var log = logf.Log.WithName("RayCluster-Controller")

// Get the port required to connect to the Ray cluster by worker nodes and drivers
// started within the cluster.
// For Ray >= 1.11.0 this is the GCS server port. For Ray < 1.11.0 it is the Redis port.
func GetHeadPort(headStartParams map[string]string) string {
	var headPort string
	if value, ok := headStartParams["port"]; !ok {
		// using default port
		headPort = strconv.Itoa(DefaultRedisPort)
	} else {
		// setting port from the params
		headPort = value
	}
	return headPort
}

// rayClusterHAEnabled check if RayCluster enabled HA in annotations
func rayClusterHAEnabled(instance rayiov1alpha1.RayCluster) bool {
	if instance.Annotations == nil {
		return false
	}
	if v, ok := instance.Annotations[RayHAEnabledAnnotationKey]; ok {
		if strings.ToLower(v) == "true" {
			return true
		}
	}
	return false
}

func initTemplateAnnotations(instance rayiov1alpha1.RayCluster, podTemplate *v1.PodTemplateSpec) {
	if podTemplate.Annotations == nil {
		podTemplate.Annotations = make(map[string]string)
	}

	// For now, we just set ray external storage enabled/disabled by checking if HA is enalled/disabled.
	// This may need to be updated in the future.
	if rayClusterHAEnabled(instance) {
		podTemplate.Annotations[RayHAEnabledAnnotationKey] = "true"
		// if we have HA enabled, we need to set up a default external storage namespace.
		podTemplate.Annotations[RayExternalStorageNSAnnotationKey] = string(instance.UID)
	} else {
		podTemplate.Annotations[RayHAEnabledAnnotationKey] = "false"
	}
	podTemplate.Annotations[RayNodeHealthStateAnnotationKey] = ""

	// set ray external storage namespace if user specified one.
	if instance.Annotations != nil {
		if v, ok := instance.Annotations[RayExternalStorageNSAnnotationKey]; ok {
			podTemplate.Annotations[RayExternalStorageNSAnnotationKey] = v
		}
	}
}

// DefaultHeadPodTemplate sets the config values
func DefaultHeadPodTemplate(instance rayiov1alpha1.RayCluster, headSpec rayiov1alpha1.HeadGroupSpec, podName string, svcName string, headPort string) v1.PodTemplateSpec {
	// TODO (Dmitri) The argument headPort is essentially unused;
	// headPort is passed into setMissingRayStartParams but unused there for the head pod.
	// To mitigate this awkwardness and reduce code redundancy, unify head and worker pod configuration logic.
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
	headSpec.RayStartParams = setMissingRayStartParams(headSpec.RayStartParams, rayiov1alpha1.HeadNode, svcName, headPort)
	headSpec.RayStartParams = setAgentListPortStartParams(instance, headSpec.RayStartParams)

	initTemplateAnnotations(instance, &podTemplate)

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

// DefaultWorkerPodTemplate sets the config values
func DefaultWorkerPodTemplate(instance rayiov1alpha1.RayCluster, workerSpec rayiov1alpha1.WorkerGroupSpec, podName string, svcName string, headPort string) v1.PodTemplateSpec {
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
	workerSpec.RayStartParams = setMissingRayStartParams(workerSpec.RayStartParams, rayiov1alpha1.WorkerNode, svcName, headPort)
	workerSpec.RayStartParams = setAgentListPortStartParams(instance, workerSpec.RayStartParams)

	initTemplateAnnotations(instance, &podTemplate)

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

func initLivenessProbeHandler(probe *v1.Probe, rayNodeType rayiov1alpha1.RayNodeType) {
	if probe.Exec == nil {
		// we only create the probe if user did not specify any.
		if rayNodeType == rayiov1alpha1.HeadNode {
			// head node liveness probe
			cmd := []string{
				"bash", "-c", fmt.Sprintf("wget -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardAgentListenPort, RayAgentRayletHealthPath),
				"&&", "bash", "-c", fmt.Sprintf("wget -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardPort, RayDashboardGCSHealthPath),
			}
			probe.Exec = &v1.ExecAction{Command: cmd}
		} else {
			// worker node liveness probe
			cmd := []string{
				"bash", "-c", fmt.Sprintf("wget -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardAgentListenPort, RayAgentRayletHealthPath),
			}
			probe.Exec = &v1.ExecAction{Command: cmd}
		}
	}
}

func initReadinessProbeHandler(probe *v1.Probe, rayNodeType rayiov1alpha1.RayNodeType) {
	if probe.Exec == nil {
		// we only create the probe if user did not specify any.
		if rayNodeType == rayiov1alpha1.HeadNode {
			// head node readiness probe
			cmd := []string{
				"bash", "-c", fmt.Sprintf("wget -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardAgentListenPort, RayAgentRayletHealthPath),
				"&&", "bash", "-c", fmt.Sprintf("wget -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardPort, RayDashboardGCSHealthPath),
			}
			probe.Exec = &v1.ExecAction{Command: cmd}
		} else {
			// worker node readiness probe
			cmd := []string{
				"bash", "-c", fmt.Sprintf("wget -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardAgentListenPort, RayAgentRayletHealthPath),
			}
			probe.Exec = &v1.ExecAction{Command: cmd}
		}
	}
}

// BuildPod a pod config
func BuildPod(podTemplateSpec v1.PodTemplateSpec, rayNodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string, svcName string, headPort string, enableRayAutoscaler *bool) (aPod v1.Pod) {
	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: podTemplateSpec.ObjectMeta,
		Spec:       podTemplateSpec.Spec,
	}
	rayContainerIndex := getRayContainerIndex(pod)

	// Add /dev/shm volumeMount for the object store to avoid performance degradation.
	addEmptyDir(&pod.Spec.Containers[rayContainerIndex], &pod, SharedMemoryVolumeName, SharedMemoryVolumeMountPath, v1.StorageMediumMemory)
	if rayNodeType == rayiov1alpha1.HeadNode && enableRayAutoscaler != nil && *enableRayAutoscaler {
		// The Ray autoscaler writes logs which are read by the Ray head.
		// We need a shared log volume to enable this information flow.
		// Specifically, this is required for the event-logging functionality
		// introduced in https://github.com/ray-project/ray/pull/13434.
		autoscalerContainerIndex := getAutoscalerContainerIndex(pod)
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
		cont := concatenateContainerCommand(rayNodeType, rayStartParams, pod.Spec.Containers[rayContainerIndex].Resources)
		// replacing the old command
		pod.Spec.Containers[rayContainerIndex].Command = []string{"/bin/bash", "-c", "--"}
		if cmd != "" {
			// If 'ray start' has --block specified, commands after it will not get executed.
			// so we need to put cmd before cont.
			args = fmt.Sprintf("%s && %s", cmd, cont)
		} else {
			args = cont
		}

		if !isRayStartWithBlock(rayStartParams) {
			// sleep infinity is used to keep the pod `running` after the last command exits, and not go into `completed` state
			args = args + " && sleep infinity"
		}

		pod.Spec.Containers[rayContainerIndex].Args = []string{args}
	}

	for index := range pod.Spec.InitContainers {
		setInitContainerEnvVars(&pod.Spec.InitContainers[index], svcName)
	}

	setContainerEnvVars(&pod, rayContainerIndex, rayNodeType, rayStartParams, svcName, headPort)

	// health check only if HA enabled
	if podTemplateSpec.Annotations != nil {
		if enabledString, ok := podTemplateSpec.Annotations[RayHAEnabledAnnotationKey]; ok {
			if strings.ToLower(enabledString) == "true" {
				// Ray HA is enabled and we need to add health checks
				if pod.Spec.Containers[rayContainerIndex].ReadinessProbe == nil {
					// it is possible that some user have the probe parameters to override the default,
					// in this case, this if condition is skipped
					probe := &v1.Probe{
						InitialDelaySeconds: DefaultReadinessProbeInitialDelaySeconds,
						TimeoutSeconds:      DefaultReadinessProbeTimeoutSeconds,
						PeriodSeconds:       DefaultReadinessProbePeriodSeconds,
						SuccessThreshold:    DefaultReadinessProbeSuccessThreshold,
						FailureThreshold:    DefaultReadinessProbeFailureThreshold,
					}
					pod.Spec.Containers[rayContainerIndex].ReadinessProbe = probe
				}
				// add readiness probe exec command in case missing.
				initReadinessProbeHandler(pod.Spec.Containers[rayContainerIndex].ReadinessProbe, rayNodeType)

				if pod.Spec.Containers[rayContainerIndex].LivenessProbe == nil {
					// it is possible that some user have the probe parameters to override the default,
					// in this case, this if condition is skipped
					probe := &v1.Probe{
						InitialDelaySeconds: DefaultLivenessProbeInitialDelaySeconds,
						TimeoutSeconds:      DefaultLivenessProbeTimeoutSeconds,
						PeriodSeconds:       DefaultLivenessProbePeriodSeconds,
						SuccessThreshold:    DefaultLivenessProbeSuccessThreshold,
						FailureThreshold:    DefaultLivenessProbeFailureThreshold,
					}
					pod.Spec.Containers[rayContainerIndex].LivenessProbe = probe
				}
				// add liveness probe exec command in case missing
				initLivenessProbeHandler(pod.Spec.Containers[rayContainerIndex].LivenessProbe, rayNodeType)
			}
		}
	}

	return pod
}

// BuildAutoscalerContainer builds a Ray autoscaler container which can be appended to the head pod.
func BuildAutoscalerContainer() v1.Container {
	container := v1.Container{
		Name: AutoscalerContainerName,
		// TODO: choose right version based on instance.spec.Version
		// The currently used image reflects the latest changes from Ray master.
		Image:           "rayproject/ray:a304d1",
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

func getRayContainerIndex(pod v1.Pod) (rayContainerIndex int) {
	// a ray pod can have multiple containers.
	// we identify the ray container based on env var: RAY=true
	// if the env var is missing, we choose containers[0].
	for i, container := range pod.Spec.Containers {
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

func getAutoscalerContainerIndex(pod v1.Pod) (autoscalerContainerIndex int) {
	// we identify the autoscaler container based on its name
	for i, container := range pod.Spec.Containers {
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
		RayNodeLabelKey:                    "yes",
		RayClusterLabelKey:                 rayClusterName,
		RayNodeTypeLabelKey:                string(rayNodeType),
		RayNodeGroupLabelKey:               groupName,
		RayIDLabelKey:                      utils.CheckLabel(utils.GenerateIdentifier(rayClusterName, rayNodeType)),
		KubernetesApplicationNameLabelKey:  ApplicationName,
		KubernetesCreatedByLabelKey:        ComponentName,
		RayClusterDashboardServiceLabelKey: utils.GenerateDashboardAgentLabel(rayClusterName),
	}

	for k, v := range ret {
		if k == string(rayNodeType) {
			// overriding invalid values for this label
			if v != string(rayiov1alpha1.HeadNode) && v != string(rayiov1alpha1.WorkerNode) {
				labels[k] = v
			}
		}
		if k == RayNodeGroupLabelKey {
			// overriding invalid values for this label
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

func setInitContainerEnvVars(container *v1.Container, svcName string) {
	// RAY_IP can be used in the DNS lookup
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []v1.EnvVar{}
	}
	if !envVarExists("RAY_IP", container.Env) {
		ip := v1.EnvVar{Name: "RAY_IP"}
		ip.Value = svcName
		container.Env = append(container.Env, ip)
	}
}

func setContainerEnvVars(pod *v1.Pod, rayContainerIndex int, rayNodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string, svcName string, headPort string) {
	// set IP to local host if head, or the the svc otherwise  RAY_IP
	// set the port RAY_PORT
	// set the password?
	container := &pod.Spec.Containers[rayContainerIndex]
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []v1.EnvVar{}
	}

	var rayIP string
	if rayNodeType == rayiov1alpha1.HeadNode {
		// if head, use localhost
		rayIP = LOCAL_HOST
	} else {
		// if worker, use the service name of the head
		rayIP = svcName
	}

	if !envVarExists(RAY_IP, container.Env) {
		ipEnv := v1.EnvVar{Name: RAY_IP, Value: rayIP}
		container.Env = append(container.Env, ipEnv)
	}
	if !envVarExists(RAY_PORT, container.Env) {
		portEnv := v1.EnvVar{Name: RAY_PORT, Value: headPort}
		container.Env = append(container.Env, portEnv)
	}
	// Setting the RAY_ADDRESS env allows connecting to Ray using ray.init() when connecting
	// from within the cluster.
	if !envVarExists(RAY_ADDRESS, container.Env) {
		rayAddress := fmt.Sprintf("%s:%s", rayIP, headPort)
		addressEnv := v1.EnvVar{Name: RAY_ADDRESS, Value: rayAddress}
		container.Env = append(container.Env, addressEnv)
	}
	if !envVarExists(REDIS_PASSWORD, container.Env) {
		// setting the REDIS_PASSWORD env var from the params
		port := v1.EnvVar{Name: REDIS_PASSWORD}
		if value, ok := rayStartParams["redis-password"]; ok {
			port.Value = value
		}
		container.Env = append(container.Env, port)
	}
	if !envVarExists(RAY_EXTERNAL_STORAGE_NS, container.Env) {
		// setting the RAY_EXTERNAL_STORAGE_NS env var from the params
		if pod.Annotations != nil {
			if v, ok := pod.Annotations[RayExternalStorageNSAnnotationKey]; ok {
				storageNS := v1.EnvVar{Name: RAY_EXTERNAL_STORAGE_NS, Value: v}
				container.Env = append(container.Env, storageNS)
			}
		}
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
func setMissingRayStartParams(rayStartParams map[string]string, nodeType rayiov1alpha1.RayNodeType, svcName string, headPort string) (completeStartParams map[string]string) {
	// Note: The argument headPort is unused for nodeType == rayiov1alpha1.HeadNode.
	if nodeType == rayiov1alpha1.WorkerNode {
		if _, ok := rayStartParams["address"]; !ok {
			address := fmt.Sprintf("%s:%s", svcName, headPort)
			rayStartParams["address"] = address
		}
	}

	// add metrics port for expose the metrics to the prometheus.
	if _, ok := rayStartParams["metrics-export-port"]; !ok {
		rayStartParams["metrics-export-port"] = fmt.Sprint(DefaultMetricsPort)
	}

	return rayStartParams
}

func setAgentListPortStartParams(instance rayiov1alpha1.RayCluster, rayStartParams map[string]string) (completeStartParams map[string]string) {
	// add dashboard listen port for serve endpoints to RayService.
	if _, ok := rayStartParams["dashboard-agent-listen-port"]; !ok {
		if value, ok := instance.Annotations[EnableAgentServiceKey]; ok && value == EnableAgentServiceTrue {
			rayStartParams["dashboard-agent-listen-port"] = strconv.Itoa(DefaultDashboardAgentListenPort)
		}
	}

	return rayStartParams
}

// concatenateContainerCommand with ray start
func concatenateContainerCommand(nodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string, resource v1.ResourceRequirements) (fullCmd string) {
	if _, ok := rayStartParams["num-cpus"]; !ok {
		cpu := resource.Limits[v1.ResourceCPU]
		if !cpu.IsZero() {
			rayStartParams["num-cpus"] = strconv.FormatInt(cpu.Value(), 10)
		}
	}

	if _, ok := rayStartParams["memory"]; !ok {
		memory := resource.Limits[v1.ResourceMemory]
		if !memory.IsZero() {
			rayStartParams["memory"] = strconv.FormatInt(memory.Value(), 10)
		}
	}

	if _, ok := rayStartParams["num-gpus"]; !ok {
		// Scan for resource keys ending with "gpu" like "nvidia.com/gpu".
		for resourceKey, resource := range resource.Limits {
			if strings.HasSuffix(string(resourceKey), "gpu") && !resource.IsZero() {
				rayStartParams["num-gpus"] = strconv.FormatInt(resource.Value(), 10)
			}
			// For now, only support one GPU type. Break on first match.
			break
		}
	}

	log.V(10).Info("concatenate container command", "ray start params", rayStartParams)

	switch nodeType {
	case rayiov1alpha1.HeadNode:
		return fmt.Sprintf("ulimit -n 65536; ray start --head %s", convertParamMap(rayStartParams))
	case rayiov1alpha1.WorkerNode:
		return fmt.Sprintf("ulimit -n 65536; ray start %s", convertParamMap(rayStartParams))
	default:
		log.Error(fmt.Errorf("missing node type"), "a node must be either head or worker")
	}
	return ""
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

// ValidateHeadRayStartParams will validate the head node's RayStartParams.
// Return a bool indicating the validity of RayStartParams and an err with additional information.
// If isValid is true, RayStartParams are valid. Any errors will only affect performance.
// If isValid is false, RayStartParams are invalid will result in an unhealthy or failed Ray cluster.
func ValidateHeadRayStartParams(rayHeadGroupSpec rayiov1alpha1.HeadGroupSpec) (isValid bool, err error) {
	// TODO (dxia): if you add more validation, please split checks into separate subroutines.
	var objectStoreMemory int64
	rayStartParams := rayHeadGroupSpec.RayStartParams
	// validation for the object store memory
	if objectStoreMemoryStr, ok := rayStartParams[ObjectStoreMemoryKey]; ok {
		objectStoreMemory, err = strconv.ParseInt(objectStoreMemoryStr, 10, 64)
		if err != nil {
			isValid = false
			err = errors.NewBadRequest(fmt.Sprintf("Cannot parse %s %s as an integer: %s", ObjectStoreMemoryKey, objectStoreMemoryStr, err.Error()))
			return
		}
		for _, container := range rayHeadGroupSpec.Template.Spec.Containers {
			// find the ray container.
			if container.Name == RayHeadContainer {
				if shmSize, ok := container.Resources.Requests.Memory().AsInt64(); ok && objectStoreMemory > shmSize {
					if envVarExists(AllowSlowStorageEnvVar, container.Env) {
						// in ray if this env var is set, it will only affect the performance.
						isValid = true
						msg := fmt.Sprintf("RayStartParams: object store memory exceeds head node container's memory request, %s:%d, memory request:%d\n"+
							"This will harm performance. Consider deleting files in %s or increasing head node's memory request.", ObjectStoreMemoryKey, objectStoreMemory, shmSize, SharedMemoryVolumeMountPath)
						log.Info(msg)
						err = errors.NewBadRequest(msg)
						return
					} else {
						// if not set, the head node may crash and result in an unhealthy status.
						isValid = false
						msg := fmt.Sprintf("RayStartParams: object store memory exceeds head node container's memory request, %s:%d, memory request:%d\n"+
							"This will lead to a ValueError in Ray! Consider deleting files in %s or increasing head node's memory request.\n"+
							"To ignore this warning, set the following environment variable in headGroupSpec: %s=1",
							ObjectStoreMemoryKey, objectStoreMemory, shmSize, SharedMemoryVolumeMountPath, AllowSlowStorageEnvVar)
						err = errors.NewBadRequest(msg)
						return
					}
				}
			}
		}
	}
	// default return
	return true, nil
}
