package common

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
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
	RayLogVolumeMountPath       = "/tmp/ray"
	AutoscalerContainerName     = "autoscaler"
	RayHeadContainer            = "ray-head"
	ObjectStoreMemoryKey        = "object-store-memory"
	// TODO (davidxia): should be a const in upstream ray-project/ray
	AllowSlowStorageEnvVar = "RAY_OBJECT_STORE_ALLOW_SLOW_STORAGE"
	// If set to true, kuberay auto injects an init container waiting for ray GCS.
	// If false, you will need to inject your own init container to ensure ray GCS is up before the ray workers start.
	EnableInitContainerInjectionEnvKey = "ENABLE_INIT_CONTAINER_INJECTION"
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

// Check if the RayCluster has GCS fault tolerance enabled.
func IsGCSFaultToleranceEnabled(instance rayv1.RayCluster) bool {
	v, ok := instance.Annotations[RayFTEnabledAnnotationKey]
	return ok && strings.ToLower(v) == "true"
}

// Check if overwrites the container command.
func isOverwriteRayContainerCmd(instance rayv1.RayCluster) bool {
	v, ok := instance.Annotations[RayOverwriteContainerCmdAnnotationKey]
	return ok && strings.ToLower(v) == "true"
}

func initTemplateAnnotations(instance rayv1.RayCluster, podTemplate *v1.PodTemplateSpec) {
	if podTemplate.Annotations == nil {
		podTemplate.Annotations = make(map[string]string)
	}

	// For now, we just set ray external storage enabled/disabled by checking if FT is enabled/disabled.
	// This may need to be updated in the future.
	if IsGCSFaultToleranceEnabled(instance) {
		podTemplate.Annotations[RayFTEnabledAnnotationKey] = "true"
		// if we have FT enabled, we need to set up a default external storage namespace.
		podTemplate.Annotations[RayExternalStorageNSAnnotationKey] = string(instance.UID)
	} else {
		podTemplate.Annotations[RayFTEnabledAnnotationKey] = "false"
	}

	if isOverwriteRayContainerCmd(instance) {
		podTemplate.Annotations[RayOverwriteContainerCmdAnnotationKey] = "true"
	}
	// set ray external storage namespace if user specified one.
	if instance.Annotations != nil {
		if v, ok := instance.Annotations[RayExternalStorageNSAnnotationKey]; ok {
			podTemplate.Annotations[RayExternalStorageNSAnnotationKey] = v
		}
	}
}

// DefaultHeadPodTemplate sets the config values
func DefaultHeadPodTemplate(instance rayv1.RayCluster, headSpec rayv1.HeadGroupSpec, podName string, headPort string) v1.PodTemplateSpec {
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
	podTemplate.Labels = labelPod(rayv1.HeadNode, instance.Name, "headgroup", instance.Spec.HeadGroupSpec.Template.ObjectMeta.Labels)
	headSpec.RayStartParams = setMissingRayStartParams(headSpec.RayStartParams, rayv1.HeadNode, headPort, "", instance.Annotations)

	initTemplateAnnotations(instance, &podTemplate)

	// if in-tree autoscaling is enabled, then autoscaler container should be injected into head pod.
	if instance.Spec.EnableInTreeAutoscaling != nil && *instance.Spec.EnableInTreeAutoscaling {
		// The default autoscaler is not compatible with Kubernetes. As a result, we disable
		// the monitor process by default and inject a KubeRay autoscaler side container into the head pod.
		headSpec.RayStartParams["no-monitor"] = "true"
		// set custom service account with proper roles bound.
		// utils.CheckName clips the name to match the behavior of reconcileAutoscalerServiceAccount
		podTemplate.Spec.ServiceAccountName = utils.CheckName(utils.GetHeadGroupServiceAccountName(&instance))
		// Use the same image as Ray head container by default.
		autoscalerImage := podTemplate.Spec.Containers[RayContainerIndex].Image
		// inject autoscaler container into head pod
		autoscalerContainer := BuildAutoscalerContainer(autoscalerImage)
		// Merge the user overrides from autoscalerOptions into the autoscaler container config.
		mergeAutoscalerOverrides(&autoscalerContainer, instance.Spec.AutoscalerOptions)
		podTemplate.Spec.Containers = append(podTemplate.Spec.Containers, autoscalerContainer)
	}

	// If the metrics port does not exist in the Ray container, add a default one for Promethues.
	isMetricsPortExists := utils.FindContainerPort(&podTemplate.Spec.Containers[RayContainerIndex], MetricsPortName, -1) != -1
	if !isMetricsPortExists {
		metricsPort := v1.ContainerPort{
			Name:          MetricsPortName,
			ContainerPort: int32(DefaultMetricsPort),
		}
		podTemplate.Spec.Containers[RayContainerIndex].Ports = append(podTemplate.Spec.Containers[RayContainerIndex].Ports, metricsPort)
	}

	return podTemplate
}

func getEnableInitContainerInjection() bool {
	if s := os.Getenv(EnableInitContainerInjectionEnvKey); strings.ToLower(s) == "false" {
		return false
	}
	return true
}

func getEnableProbesInjection() bool {
	if s := os.Getenv(ENABLE_PROBES_INJECTION); strings.ToLower(s) == "false" {
		return false
	}
	return true
}

// DefaultWorkerPodTemplate sets the config values
func DefaultWorkerPodTemplate(instance rayv1.RayCluster, workerSpec rayv1.WorkerGroupSpec, podName string, fqdnRayIP string, headPort string) v1.PodTemplateSpec {
	podTemplate := workerSpec.Template
	podTemplate.GenerateName = podName
	if podTemplate.ObjectMeta.Namespace == "" {
		podTemplate.ObjectMeta.Namespace = instance.Namespace
		log.Info("Setting pod namespaces", "namespace", instance.Namespace)
	}

	// The Ray worker should only start once the GCS server is ready.
	// only inject init container only when ENABLE_INIT_CONTAINER_INJECTION is true
	enableInitContainerInjection := getEnableInitContainerInjection()

	if enableInitContainerInjection {
		// Do not modify `deepCopyRayContainer` anywhere.
		deepCopyRayContainer := podTemplate.Spec.Containers[RayContainerIndex].DeepCopy()
		initContainer := v1.Container{
			Name:            "wait-gcs-ready",
			Image:           podTemplate.Spec.Containers[RayContainerIndex].Image,
			ImagePullPolicy: podTemplate.Spec.Containers[RayContainerIndex].ImagePullPolicy,
			Command:         []string{"/bin/bash", "-lc", "--"},
			Args: []string{
				fmt.Sprintf(`
					SECONDS=0
					while true; do
						if (( SECONDS <= 120 )); then
							if ray health-check --address %s:%s > /dev/null 2>&1; then
								echo "GCS is ready."
								break
							fi
							echo "$SECONDS seconds elapsed: Waiting for GCS to be ready."
						else
							if ray health-check --address %s:%s; then
								echo "GCS is ready. Any error messages above can be safely ignored."
								break
							fi
							echo "$SECONDS seconds elapsed: Still waiting for GCS to be ready. For troubleshooting, refer to the FAQ at https://github.com/ray-project/kuberay/blob/master/docs/guidance/FAQ.md."
						fi
						sleep 5		
					done
				`, fqdnRayIP, headPort, fqdnRayIP, headPort),
			},
			SecurityContext: podTemplate.Spec.Containers[RayContainerIndex].SecurityContext.DeepCopy(),
			// This init container requires certain environment variables to establish a secure connection with the Ray head using TLS authentication.
			// Additionally, some of these environment variables may reference files stored in volumes, so we need to include both the `Env` and `VolumeMounts` fields here.
			// For more details, please refer to: https://docs.ray.io/en/latest/ray-core/configure.html#tls-authentication.
			Env:          deepCopyRayContainer.Env,
			VolumeMounts: deepCopyRayContainer.VolumeMounts,
			// If users specify a ResourceQuota for the namespace, the init container needs to specify resources explicitly.
			// GKE's Autopilot does not support GPU-using init containers, so we explicitly specify the resources for the
			// init container instead of reusing the resources of the Ray container.
			Resources: v1.ResourceRequirements{
				// The init container's resource consumption remains constant, as it solely sends requests to check the GCS status at a fixed frequency.
				// Therefore, hard-coding the resources is acceptable.
				Limits: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("200m"),
					v1.ResourceMemory: resource.MustParse("256Mi"),
				},
				Requests: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("200m"),
					v1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
		}
		podTemplate.Spec.InitContainers = append(podTemplate.Spec.InitContainers, initContainer)
	}
	// If the replica of workers is more than 1, `ObjectMeta.Name` may cause name conflict errors.
	// Hence, we set `ObjectMeta.Name` to an empty string, and use GenerateName to prevent name conflicts.
	podTemplate.ObjectMeta.Name = ""
	if podTemplate.Labels == nil {
		podTemplate.Labels = make(map[string]string)
	}
	podTemplate.Labels = labelPod(rayv1.WorkerNode, instance.Name, workerSpec.GroupName, workerSpec.Template.ObjectMeta.Labels)
	workerSpec.RayStartParams = setMissingRayStartParams(workerSpec.RayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, instance.Annotations)

	initTemplateAnnotations(instance, &podTemplate)

	// If the metrics port does not exist in the Ray container, add a default one for Promethues.
	isMetricsPortExists := utils.FindContainerPort(&podTemplate.Spec.Containers[RayContainerIndex], MetricsPortName, -1) != -1
	if !isMetricsPortExists {
		metricsPort := v1.ContainerPort{
			Name:          MetricsPortName,
			ContainerPort: int32(DefaultMetricsPort),
		}
		podTemplate.Spec.Containers[RayContainerIndex].Ports = append(podTemplate.Spec.Containers[RayContainerIndex].Ports, metricsPort)
	}

	return podTemplate
}

// For KubeRay, the liveness and readiness probes perform the same checks.
// Hence, we use the same function to initialize both probes.
func initHealthProbe(probe *v1.Probe, rayNodeType rayv1.RayNodeType) {
	// If users do not specify probe handlers, we will set `Exec` as the default probe handler.
	if probe.Exec == nil && probe.HTTPGet == nil && probe.TCPSocket == nil && probe.GRPC == nil {
		// Case 1: head node => Check GCS and Raylet status.
		// Case 2: worker node => Check Raylet status.
		//
		// Note: Since the Raylet process and the dashboard agent process are fate-sharing,
		// we only need to check one of them. The probes use the dashboard agent's API endpoint
		// to check the health of the Raylet process.
		// TODO (kevin85421): Should we take the dashboard process into account?
		if rayNodeType == rayv1.HeadNode {
			cmd := []string{
				"bash", "-c", fmt.Sprintf("wget -T 2 -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardAgentListenPort, RayAgentRayletHealthPath),
				"&&", "bash", "-c", fmt.Sprintf("wget -T 2 -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardPort, RayDashboardGCSHealthPath),
			}
			probe.Exec = &v1.ExecAction{Command: cmd}
		} else {
			cmd := []string{
				"bash", "-c", fmt.Sprintf("wget -T 2 -q -O- http://localhost:%d/%s | grep success",
					DefaultDashboardAgentListenPort, RayAgentRayletHealthPath),
			}
			probe.Exec = &v1.ExecAction{Command: cmd}
		}
	}
}

// BuildPod a pod config
func BuildPod(podTemplateSpec v1.PodTemplateSpec, rayNodeType rayv1.RayNodeType, rayStartParams map[string]string, headPort string, enableRayAutoscaler *bool, creator string, fqdnRayIP string, enableServeService bool) (aPod v1.Pod) {
	if enableServeService {
		// TODO (kevin85421): In the current RayService implementation, we only add this label to a Pod after
		// it passes the health check. The other option is to use the readiness probe to control it. This
		// logic always add the label to the Pod no matter whether it is ready or not.
		podTemplateSpec.Labels[RayClusterServingServiceLabelKey] = EnableRayClusterServingServiceTrue
	}
	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: podTemplateSpec.ObjectMeta,
		Spec:       podTemplateSpec.Spec,
	}

	// Add /dev/shm volumeMount for the object store to avoid performance degradation.
	addEmptyDir(&pod.Spec.Containers[RayContainerIndex], &pod, SharedMemoryVolumeName, SharedMemoryVolumeMountPath, v1.StorageMediumMemory)
	if rayNodeType == rayv1.HeadNode && enableRayAutoscaler != nil && *enableRayAutoscaler {
		// The Ray autoscaler writes logs which are read by the Ray head.
		// We need a shared log volume to enable this information flow.
		// Specifically, this is required for the event-logging functionality
		// introduced in https://github.com/ray-project/ray/pull/13434.
		autoscalerContainerIndex := getAutoscalerContainerIndex(pod)
		addEmptyDir(&pod.Spec.Containers[RayContainerIndex], &pod, RayLogVolumeName, RayLogVolumeMountPath, v1.StorageMediumDefault)
		addEmptyDir(&pod.Spec.Containers[autoscalerContainerIndex], &pod, RayLogVolumeName, RayLogVolumeMountPath, v1.StorageMediumDefault)
	}
	cleanupInvalidVolumeMounts(&pod.Spec.Containers[RayContainerIndex], &pod)
	if len(pod.Spec.InitContainers) > RayContainerIndex {
		cleanupInvalidVolumeMounts(&pod.Spec.InitContainers[RayContainerIndex], &pod)
	}

	var cmd, args string
	if len(pod.Spec.Containers[RayContainerIndex].Command) > 0 {
		cmd = convertCmdToString(pod.Spec.Containers[RayContainerIndex].Command)
	}
	if len(pod.Spec.Containers[RayContainerIndex].Args) > 0 {
		cmd += convertCmdToString(pod.Spec.Containers[RayContainerIndex].Args)
	}

	// Increase the open file descriptor limit of the `ray start` process and its child processes to 65536.
	ulimitCmd := "ulimit -n 65536"
	// Generate the `ray start` command.
	rayStartCmd := generateRayStartCommand(rayNodeType, rayStartParams, pod.Spec.Containers[RayContainerIndex].Resources)

	// Check if overwrites the generated container command or not.
	isOverwriteRayContainerCmd := false
	if v, ok := podTemplateSpec.Annotations[RayOverwriteContainerCmdAnnotationKey]; ok {
		isOverwriteRayContainerCmd = strings.ToLower(v) == "true"
	}

	// TODO (kevin85421): Consider removing the check for the "ray start" string in the future.
	if !isOverwriteRayContainerCmd && !strings.Contains(cmd, "ray start") {
		generatedCmd := fmt.Sprintf("%s; %s", ulimitCmd, rayStartCmd)
		log.Info("BuildPod", "rayNodeType", rayNodeType, "generatedCmd", generatedCmd)
		// replacing the old command
		pod.Spec.Containers[RayContainerIndex].Command = []string{"/bin/bash", "-lc", "--"}
		if cmd != "" {
			// If 'ray start' has --block specified, commands after it will not get executed.
			// so we need to put cmd before cont.
			args = fmt.Sprintf("%s && %s", cmd, generatedCmd)
		} else {
			args = generatedCmd
		}

		if !isRayStartWithBlock(rayStartParams) {
			// sleep infinity is used to keep the pod `running` after the last command exits, and not go into `completed` state
			args = args + " && sleep infinity"
		}
		pod.Spec.Containers[RayContainerIndex].Args = []string{args}
	}

	for index := range pod.Spec.InitContainers {
		setInitContainerEnvVars(&pod.Spec.InitContainers[index], fqdnRayIP)
	}
	setContainerEnvVars(&pod, rayNodeType, rayStartParams, fqdnRayIP, headPort, rayStartCmd, creator)

	// Inject probes into the Ray containers if the user has not explicitly disabled them.
	// The feature flag `ENABLE_PROBES_INJECTION` will be removed if this feature is stable enough.
	enableProbesInjection := getEnableProbesInjection()
	log.Info("Probes injection feature flag", "enabled", enableProbesInjection)
	if enableProbesInjection {
		// Configure the readiness and liveness probes for the Ray container. These probes
		// play a crucial role in KubeRay health checks. Without them, certain failures,
		// such as the Raylet process crashing, may go undetected.
		if pod.Spec.Containers[RayContainerIndex].ReadinessProbe == nil {
			probe := &v1.Probe{
				InitialDelaySeconds: DefaultReadinessProbeInitialDelaySeconds,
				TimeoutSeconds:      DefaultReadinessProbeTimeoutSeconds,
				PeriodSeconds:       DefaultReadinessProbePeriodSeconds,
				SuccessThreshold:    DefaultReadinessProbeSuccessThreshold,
				FailureThreshold:    DefaultReadinessProbeFailureThreshold,
			}
			pod.Spec.Containers[RayContainerIndex].ReadinessProbe = probe
		}
		initHealthProbe(pod.Spec.Containers[RayContainerIndex].ReadinessProbe, rayNodeType)

		if pod.Spec.Containers[RayContainerIndex].LivenessProbe == nil {
			probe := &v1.Probe{
				InitialDelaySeconds: DefaultLivenessProbeInitialDelaySeconds,
				TimeoutSeconds:      DefaultLivenessProbeTimeoutSeconds,
				PeriodSeconds:       DefaultLivenessProbePeriodSeconds,
				SuccessThreshold:    DefaultLivenessProbeSuccessThreshold,
				FailureThreshold:    DefaultLivenessProbeFailureThreshold,
			}
			pod.Spec.Containers[RayContainerIndex].LivenessProbe = probe
		}
		initHealthProbe(pod.Spec.Containers[RayContainerIndex].LivenessProbe, rayNodeType)
	}

	return pod
}

// BuildAutoscalerContainer builds a Ray autoscaler container which can be appended to the head pod.
func BuildAutoscalerContainer(autoscalerImage string) v1.Container {
	container := v1.Container{
		Name:            AutoscalerContainerName,
		Image:           autoscalerImage,
		ImagePullPolicy: v1.PullIfNotPresent,
		Env: []v1.EnvVar{
			{
				Name: RAY_CLUSTER_NAME,
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf("metadata.labels['%s']", RayClusterLabelKey),
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
			{
				Name: "RAY_HEAD_POD_NAME",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
			{
				Name:  "KUBERAY_CRD_VER",
				Value: "v1",
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
		Resources: v1.ResourceRequirements{
			Limits: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("500m"),
				v1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("500m"),
				v1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	}
	return container
}

// Merge the user overrides from autoscalerOptions into the autoscaler container config.
func mergeAutoscalerOverrides(autoscalerContainer *v1.Container, autoscalerOptions *rayv1.AutoscalerOptions) {
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
		if len(autoscalerOptions.Env) > 0 {
			autoscalerContainer.Env = append(autoscalerContainer.Env, autoscalerOptions.Env...)
		}
		if len(autoscalerOptions.EnvFrom) > 0 {
			autoscalerContainer.EnvFrom = append(autoscalerContainer.EnvFrom, autoscalerOptions.EnvFrom...)
		}
		if len(autoscalerOptions.VolumeMounts) > 0 {
			autoscalerContainer.VolumeMounts = append(autoscalerContainer.VolumeMounts, autoscalerOptions.VolumeMounts...)
		}
		if autoscalerOptions.SecurityContext != nil {
			autoscalerContainer.SecurityContext = autoscalerOptions.SecurityContext.DeepCopy()
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
func labelPod(rayNodeType rayv1.RayNodeType, rayClusterName string, groupName string, labels map[string]string) (ret map[string]string) {
	if labels == nil {
		labels = make(map[string]string)
	}

	ret = map[string]string{
		RayNodeLabelKey:                   "yes",
		RayClusterLabelKey:                rayClusterName,
		RayNodeTypeLabelKey:               string(rayNodeType),
		RayNodeGroupLabelKey:              groupName,
		RayIDLabelKey:                     utils.CheckLabel(utils.GenerateIdentifier(rayClusterName, rayNodeType)),
		KubernetesApplicationNameLabelKey: ApplicationName,
		KubernetesCreatedByLabelKey:       ComponentName,
	}

	for k, v := range ret {
		if k == string(rayNodeType) {
			// overriding invalid values for this label
			if v != string(rayv1.HeadNode) && v != string(rayv1.WorkerNode) {
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

func setInitContainerEnvVars(container *v1.Container, fqdnRayIP string) {
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []v1.EnvVar{}
	}
	// Init containers in both head and worker require FQ_RAY_IP.
	// (1) The head needs FQ_RAY_IP to create a self-signed certificate for its TLS authenticate.
	// (2) The worker needs FQ_RAY_IP to establish a connection with the Ray head.
	container.Env = append(container.Env,
		v1.EnvVar{Name: FQ_RAY_IP, Value: fqdnRayIP},
		// RAY_IP is deprecated and should be kept for backward compatibility purposes only.
		v1.EnvVar{Name: RAY_IP, Value: utils.ExtractRayIPFromFQDN(fqdnRayIP)},
	)
}

func setContainerEnvVars(pod *v1.Pod, rayNodeType rayv1.RayNodeType, rayStartParams map[string]string, fqdnRayIP string, headPort string, rayStartCmd string, creator string) {
	// TODO: Audit all environment variables to identify which should not be modified by users.
	container := &pod.Spec.Containers[RayContainerIndex]
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []v1.EnvVar{}
	}

	// case 1: head   => Use LOCAL_HOST
	// case 2: worker => Use fqdnRayIP (fully qualified domain name)
	ip := LOCAL_HOST
	if rayNodeType == rayv1.WorkerNode {
		ip = fqdnRayIP
		container.Env = append(container.Env,
			v1.EnvVar{Name: FQ_RAY_IP, Value: ip},
			// RAY_IP is deprecated and should be kept for backward compatibility purposes only.
			v1.EnvVar{Name: RAY_IP, Value: utils.ExtractRayIPFromFQDN(ip)},
		)
	}

	// The RAY_CLUSTER_NAME environment variable is managed by KubeRay and should not be set by the user.
	clusterNameEnv := v1.EnvVar{
		Name: RAY_CLUSTER_NAME,
		ValueFrom: &v1.EnvVarSource{
			FieldRef: &v1.ObjectFieldSelector{
				FieldPath: fmt.Sprintf("metadata.labels['%s']", RayClusterLabelKey),
			},
		},
	}
	container.Env = append(container.Env, clusterNameEnv)

	// KUBERAY_GEN_RAY_START_CMD stores the `ray start` command generated by KubeRay.
	// See https://github.com/ray-project/kuberay/issues/1560 for more details.
	generatedRayStartCmdEnv := v1.EnvVar{Name: KUBERAY_GEN_RAY_START_CMD, Value: rayStartCmd}
	container.Env = append(container.Env, generatedRayStartCmdEnv)

	if !envVarExists(RAY_PORT, container.Env) {
		portEnv := v1.EnvVar{Name: RAY_PORT, Value: headPort}
		container.Env = append(container.Env, portEnv)
	}

	if strings.ToLower(creator) == RayServiceCreatorLabelValue {
		// Only add this env for Ray Service cluster to improve service SLA.
		if !envVarExists(RAY_TIMEOUT_MS_TASK_WAIT_FOR_DEATH_INFO, container.Env) {
			deathEnv := v1.EnvVar{Name: RAY_TIMEOUT_MS_TASK_WAIT_FOR_DEATH_INFO, Value: "0"}
			container.Env = append(container.Env, deathEnv)
		}
		if !envVarExists(RAY_GCS_SERVER_REQUEST_TIMEOUT_SECONDS, container.Env) {
			gcsTimeoutEnv := v1.EnvVar{Name: RAY_GCS_SERVER_REQUEST_TIMEOUT_SECONDS, Value: "5"}
			container.Env = append(container.Env, gcsTimeoutEnv)
		}
		if !envVarExists(RAY_SERVE_KV_TIMEOUT_S, container.Env) {
			serveKvTimeoutEnv := v1.EnvVar{Name: RAY_SERVE_KV_TIMEOUT_S, Value: "5"}
			container.Env = append(container.Env, serveKvTimeoutEnv)
		}
		if !envVarExists(SERVE_CONTROLLER_PIN_ON_NODE, container.Env) {
			servePinOnNode := v1.EnvVar{Name: SERVE_CONTROLLER_PIN_ON_NODE, Value: "0"}
			container.Env = append(container.Env, servePinOnNode)
		}
	}
	// Setting the RAY_ADDRESS env allows connecting to Ray using ray.init() when connecting
	// from within the cluster.
	if !envVarExists(RAY_ADDRESS, container.Env) {
		rayAddress := fmt.Sprintf("%s:%s", ip, headPort)
		addressEnv := v1.EnvVar{Name: RAY_ADDRESS, Value: rayAddress}
		container.Env = append(container.Env, addressEnv)
	}
	if !envVarExists(RAY_USAGE_STATS_KUBERAY_IN_USE, container.Env) {
		usageEnv := v1.EnvVar{Name: RAY_USAGE_STATS_KUBERAY_IN_USE, Value: "1"}
		container.Env = append(container.Env, usageEnv)
	}
	if !envVarExists(REDIS_PASSWORD, container.Env) {
		// setting the REDIS_PASSWORD env var from the params
		redisPasswordEnv := v1.EnvVar{Name: REDIS_PASSWORD}
		if value, ok := rayStartParams["redis-password"]; ok {
			redisPasswordEnv.Value = value
		}
		container.Env = append(container.Env, redisPasswordEnv)
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
	if !envVarExists(RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, container.Env) && rayNodeType == rayv1.WorkerNode {
		// If GCS FT is enabled and RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S is not set, set the worker's
		// RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S to 600s. If the worker cannot reconnect to GCS within
		// 600s, the Raylet will exit the process. By default, the value is 60s, so the head node will
		// crash if the GCS server is down for more than 60s. Typically, the new GCS server will be available
		// in 120 seconds, so we set the timeout to 600s to avoid the worker nodes crashing.
		if ftEnabled := pod.Annotations[RayFTEnabledAnnotationKey] == "true"; ftEnabled {
			gcsTimeout := v1.EnvVar{Name: RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, Value: DefaultWorkerRayGcsReconnectTimeoutS}
			container.Env = append(container.Env, gcsTimeout)
		}
	}
	if !envVarExists(RAY_DASHBOARD_ENABLE_K8S_DISK_USAGE, container.Env) {
		// This flag enables the display of disk usage. Without this flag, the dashboard will not show disk usage.
		container.Env = append(container.Env, v1.EnvVar{Name: RAY_DASHBOARD_ENABLE_K8S_DISK_USAGE, Value: "1"})
	}
}

func envVarExists(envName string, envVars []v1.EnvVar) bool {
	for _, env := range envVars {
		if env.Name == envName {
			return true
		}
	}
	return false
}

func setMissingRayStartParams(rayStartParams map[string]string, nodeType rayv1.RayNodeType, headPort string, fqdnRayIP string, annotations map[string]string) (completeStartParams map[string]string) {
	// Note: The argument headPort is unused for nodeType == rayv1.HeadNode.
	if nodeType == rayv1.WorkerNode {
		if _, ok := rayStartParams["address"]; !ok {
			address := fmt.Sprintf("%s:%s", fqdnRayIP, headPort)
			rayStartParams["address"] = address
		}
	}

	if nodeType == rayv1.HeadNode {
		// Allow incoming connections from all network interfaces for the dashboard by default.
		// The default value of `dashboard-host` is `localhost` which is not accessible from outside the head Pod.
		if _, ok := rayStartParams["dashboard-host"]; !ok {
			rayStartParams["dashboard-host"] = "0.0.0.0"
		}

		// If `autoscaling-config` is not provided in the head Pod's rayStartParams, the `BASE_READONLY_CONFIG`
		// will be used to initialize the monitor with a READONLY autoscaler which only mirrors what the GCS tells it.
		// See `monitor.py` in Ray repository for more details.
		if _, ok := rayStartParams["autoscaling-config"]; ok {
			log.Info("Detect autoscaling-config in head Pod's rayStartParams. " +
				"The monitor process will initialize the monitor with the provided config. " +
				"Please ensure the autoscaler is set to READONLY mode.")
		}
	}

	// Add a metrics port to expose the metrics to Prometheus.
	if _, ok := rayStartParams["metrics-export-port"]; !ok {
		rayStartParams["metrics-export-port"] = fmt.Sprint(DefaultMetricsPort)
	}

	// Add --block option. See https://github.com/ray-project/kuberay/pull/675
	if _, ok := rayStartParams["block"]; !ok {
		rayStartParams["block"] = "true"
	}

	// Add dashboard listen port for RayService.
	if _, ok := rayStartParams["dashboard-agent-listen-port"]; !ok {
		if value, ok := annotations[EnableServeServiceKey]; ok && value == EnableServeServiceTrue {
			rayStartParams["dashboard-agent-listen-port"] = strconv.Itoa(DefaultDashboardAgentListenPort)
		}
	}

	return rayStartParams
}

func generateRayStartCommand(nodeType rayv1.RayNodeType, rayStartParams map[string]string, resource v1.ResourceRequirements) string {
	log.Info("generateRayStartCommand", "nodeType", nodeType, "rayStartParams", rayStartParams, "Ray container resource", resource)
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
				// For now, only support one GPU type. Break on first match.
				break
			}
		}
	}

	rayStartCmd := ""
	switch nodeType {
	case rayv1.HeadNode:
		rayStartCmd = fmt.Sprintf("ray start --head %s", convertParamMap(rayStartParams))
	case rayv1.WorkerNode:
		rayStartCmd = fmt.Sprintf("ray start %s", convertParamMap(rayStartParams))
	default:
		log.Error(fmt.Errorf("missing node type"), "a node must be either head or worker")
	}
	log.Info("generateRayStartCommand", "rayStartCmd", rayStartCmd)
	return rayStartCmd
}

func convertParamMap(rayStartParams map[string]string) (s string) {
	flags := new(bytes.Buffer)
	// specialParameterOptions' arguments can be true or false.
	// For example, --log-color can be auto | false | true.
	specialParameterOptions := []string{"log-color", "include-dashboard"}
	for option, argument := range rayStartParams {
		if utils.Contains([]string{"true", "false"}, strings.ToLower(argument)) && !utils.Contains(specialParameterOptions, option) {
			// booleanOptions: do not require any argument. Essentially represent boolean on-off switches.
			if strings.ToLower(argument) == "true" {
				fmt.Fprintf(flags, " --%s ", option)
			}
		} else {
			// parameterOption: require arguments to be provided along with the option.
			fmt.Fprintf(flags, " --%s=%s ", option, argument)
		}
	}
	return flags.String()
}

// addEmptyDir adds an emptyDir volume to the pod and a corresponding volume mount to the container
// Used for a /dev/shm memory mount for object store and for a /tmp/ray disk mount for autoscaler logs.
func addEmptyDir(container *v1.Container, pod *v1.Pod, volumeName string, volumeMountPath string, storageMedium v1.StorageMedium) {
	if checkIfVolumeMounted(container, pod, volumeMountPath) {
		log.Info("volume already mounted", "volume", volumeName, "path", volumeMountPath)
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
			return true
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
	k := 0
	for _, mountedVol := range container.VolumeMounts {
		for _, podVolume := range pod.Spec.Volumes {
			if mountedVol.Name == podVolume.Name {
				// valid mount, moving on...
				container.VolumeMounts[k] = mountedVol
				k++
				break
			}
		}
	}
	container.VolumeMounts = container.VolumeMounts[:k]
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
func ValidateHeadRayStartParams(rayHeadGroupSpec rayv1.HeadGroupSpec) (isValid bool, err error) {
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
