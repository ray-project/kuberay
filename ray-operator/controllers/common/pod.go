package common

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/utils"

	"k8s.io/apimachinery/pkg/api/resource"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SharedMemoryVolumeName      = "shared-mem"
	SharedMemoryVolumeMountPath = "/dev/shm"
)

var (
	log = logf.Log.WithName("RayCluster-Controller")
)

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

	return podTemplate
}

// BuildPod a pod config
func BuildPod(podTemplateSpec v1.PodTemplateSpec, rayNodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string, svcName string) (aPod v1.Pod) {
	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: podTemplateSpec.ObjectMeta,
		Spec:       podTemplateSpec.Spec,
	}
	index := getRayContainerIndex(pod)
	cont := concatenateContainerCommand(rayNodeType, rayStartParams)

	addEmptyDir(&pod.Spec.Containers[index], &pod)
	cleanupInvalidVolumeMounts(&pod.Spec.Containers[index], &pod)
	if len(pod.Spec.InitContainers) > index {
		cleanupInvalidVolumeMounts(&pod.Spec.InitContainers[index], &pod)
	}

	// saving temporarily the old command and args
	var cmd, args string
	if len(pod.Spec.Containers[index].Command) > 0 {
		cmd = convertCmdToString(pod.Spec.Containers[index].Command)
	}
	if len(pod.Spec.Containers[index].Args) > 0 {
		cmd += convertCmdToString(pod.Spec.Containers[index].Args)
	}
	if !strings.Contains(cmd, "ray start") {
		// replacing the old command
		pod.Spec.Containers[index].Command = []string{"/bin/bash", "-c", "--"}
		if cmd != "" {
			// sleep infinity is used to keep the pod `running` after the last command exits, and not go into `completed` state
			args = fmt.Sprintf("%s && %s && %s", cont, cmd, "sleep infinity")
		} else {
			args = fmt.Sprintf("%s && %s", cont, "sleep infinity")
		}

		pod.Spec.Containers[index].Args = []string{args}
	}
	for index := range pod.Spec.InitContainers {
		setInitContainerEnvVars(&pod.Spec.InitContainers[index], svcName)
	}

	setContainerEnvVars(&pod.Spec.Containers[index], rayNodeType, rayStartParams, svcName)

	return pod
}

func convertCmdToString(cmdArr []string) (cmd string) {
	cmdAggr := new(bytes.Buffer)
	for _, v := range cmdArr {
		fmt.Fprintf(cmdAggr, " %s ", v)
	}
	return cmdAggr.String()

}

func getRayContainerIndex(pod v1.Pod) (index int) {
	// theoretically, a ray pod can have multiple containers.
	// we identify the ray container based on env var: RAY=true
	// if the env var is missing, we choose containers[0].
	for i, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == strings.ToLower("ray") && env.Value == strings.ToLower("true") {
				return i
			}
		}
	}
	//not found, use first container
	return 0
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
			// overriding invalide values for this label
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

func setInitContainerEnvVars(container *v1.Container, svcName string) {
	//RAY_IP can be used in the DNS lookup
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []v1.EnvVar{}
	}
	if !envVarExists("RAY_IP", container.Env) {
		ip := v1.EnvVar{Name: "RAY_IP"}
		ip.Value = svcName
		container.Env = append(container.Env, ip)
	}
}

func setContainerEnvVars(container *v1.Container, rayNodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string, svcName string) {
	// set IP to local host if head, or the the svc otherwise  RAY_IP
	// set the port RAY_PORT
	// set the password?
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []v1.EnvVar{}
	}
	if !envVarExists(RAY_IP, container.Env) {
		ip := v1.EnvVar{Name: RAY_IP}
		if rayNodeType == rayiov1alpha1.HeadNode {
			// if head, use localhost
			ip.Value = "127.0.0.1"
		} else {
			// if worker, use the service name of the head
			ip.Value = svcName
		}
		container.Env = append(container.Env, ip)
	}
	if !envVarExists(RAY_PORT, container.Env) {
		port := v1.EnvVar{Name: RAY_PORT}
		if value, ok := rayStartParams["port"]; !ok {
			// using default port
			port.Value = strconv.Itoa(DefaultRedisPort)
		} else {
			// setting the RAY_PORT env var from the params
			port.Value = value
		}
		container.Env = append(container.Env, port)
	}
	if !envVarExists(REDIS_PASSWORD, container.Env) {
		// setting the REDIS_PASSWORD env var from the params
		port := v1.EnvVar{Name: REDIS_PASSWORD}
		if value, ok := rayStartParams["redis-password"]; ok {
			port.Value = value
		}
		container.Env = append(container.Env, port)
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

//TODO auto complete params
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
	return rayStartParams
}

// concatenateContainerCommand with ray start
func concatenateContainerCommand(nodeType rayiov1alpha1.RayNodeType, rayStartParams map[string]string) (fullCmd string) {
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
		fmt.Fprintf(flags, " --%s=%s ", k, v)
	}
	return flags.String()
}

// addEmptyDir add an emptyDir to the shared memory mount point /dev/shm
// this is to avoid: "The object store is using /tmp instead of /dev/shm because /dev/shm has only 67108864 bytes available. This may slow down performance!...""
func addEmptyDir(container *v1.Container, pod *v1.Pod) {
	if checkIfVolumeMounted(container, pod) {
		return
	}
	//1) create a Volume of type emptyDir and add it to Volumes
	emptyDirVolume := v1.Volume{
		Name: SharedMemoryVolumeName,
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium:    v1.StorageMediumMemory,
				SizeLimit: findMemoryReqOrLimit(*container),
			},
		},
	}
	if !checkIfVolumeMounted(container, pod) {
		pod.Spec.Volumes = append(pod.Spec.Volumes, emptyDirVolume)
	}

	//2) create a VolumeMount that uses the emptyDir
	mountedVolume := v1.VolumeMount{
		MountPath: SharedMemoryVolumeMountPath,
		Name:      SharedMemoryVolumeName,
		ReadOnly:  false,
	}
	if !checkIfVolumeMounted(container, pod) {
		container.VolumeMounts = append(container.VolumeMounts, mountedVolume)
	}
}

func checkIfVolumeMounted(container *v1.Container, pod *v1.Pod) bool {
	for _, mountedVol := range container.VolumeMounts {
		if mountedVol.MountPath == SharedMemoryVolumeMountPath {
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
			//remove the VolumeMount
			container.VolumeMounts[index] = container.VolumeMounts[len(container.VolumeMounts)-1]
			container.VolumeMounts = container.VolumeMounts[:len(container.VolumeMounts)-1]
		}
	}
}

func findMemoryReqOrLimit(container v1.Container) (res *resource.Quantity) {
	var mem *resource.Quantity
	//check the requests, if they are not set, check the limits.
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
