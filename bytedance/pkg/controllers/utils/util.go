package utils

import (
	"bytes"
	"fmt"

	rayiov1alpha1 "github.com/ray-project/ray-contrib/bytedance/pkg/api/v1alpha1"
	"github.com/ray-project/ray-contrib/bytedance/pkg/controllers/common"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

const (
	SharedMemoryMountPath  = "/dev/shm"
	SharedMemoryVolumeName = "shared-memory"
)

func GenerateServiceName(name string) string {
	return fmt.Sprintf("%s-%s", name, rayiov1alpha1.HeadNode)
}

// TODO (Jeffwan@): since user have some controls on it, its behavior is not guaranteeded.
// FindRayContainerIndex find ray container's index.
func FindRayContainerIndex(spec corev1.PodSpec) (index int) {
	// We only support one container at this moment. We definitely need a better way to filter out sidecar containers.
	if len(spec.Containers) > 1 {
		logrus.Warnf("Pod has multiple containers, we choose index=0 as Ray container")
	}
	return 0
}

// The function labelsForCluster returns the labels for selecting the resources
// belonging to the given RayCluster CR name.
func GeneratePodLabels(rayNodeType string, rayClusterName string, groupName string, labels map[string]string) (reserveLabels map[string]string) {
	reserveLabels = map[string]string{
		common.RayClusterLabelKey:   rayClusterName,
		common.RayNodeTypeLabelKey:  rayNodeType,
		common.RayNodeGroupLabelKey: groupName,
		common.RayIDLabelKey:        fmt.Sprintf("%s-%s", rayClusterName, rayNodeType),
	}
	if labels == nil {
		labels = map[string]string{}
	}

	for k, v := range reserveLabels {
		if k == rayNodeType {
			// overriding invalid values for this label
			if v != string(rayiov1alpha1.HeadNode) && v != string(rayiov1alpha1.WorkerNode) {
				labels[k] = v
			}
		}
		if k == common.RayNodeGroupLabelKey {
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

func SetInitContainerEnvs(container *corev1.Container, svcName string) {
	// RAY_HEAD_SERVICE_IP is used in the DNS lookup
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []corev1.EnvVar{}
	}
	if !envVarExists(common.RayHeadServiceIpEnv, container.Env) {
		headSvcEnv := corev1.EnvVar{Name: common.RayHeadServiceIpEnv, Value: fmt.Sprintf("%s", svcName)}
		container.Env = append(container.Env, headSvcEnv)
	}
}

func SetContainerEnvs(container *corev1.Container, rayNodeType rayiov1alpha1.RayNodeType, params map[string]string, svcName string) {
	if container.Env == nil || len(container.Env) == 0 {
		container.Env = []corev1.EnvVar{}
	}
	if !envVarExists(common.RayHeadServiceIpEnv, container.Env) {
		ip := corev1.EnvVar{Name: common.RayHeadServiceIpEnv}
		if rayNodeType == rayiov1alpha1.HeadNode {
			ip.Value = "127.0.0.1"
		} else {
			ip.Value = fmt.Sprintf("%s", svcName)
		}
		container.Env = append(container.Env, ip)
	}
	if !envVarExists(common.RayHeadServicePortEnv, container.Env) {
		port := corev1.EnvVar{Name: common.RayHeadServicePortEnv}
		if value, ok := params["port"]; !ok {
			port.Value = string(common.DefaultRedisPort)
		} else {
			port.Value = value
		}
		container.Env = append(container.Env, port)
	}
	if !envVarExists(common.RayRedisPasswordEnv, container.Env) {
		// setting the REDIS_PASSWORD env var from the params
		port := corev1.EnvVar{Name: common.RayRedisPasswordEnv}
		if value, ok := params["redis-password"]; ok {
			port.Value = value
		}
		container.Env = append(container.Env, port)
	}
}

func envVarExists(envName string, envVars []corev1.EnvVar) bool {
	if envVars == nil || len(envVars) == 0 {
		return false
	}

	for _, env := range envVars {
		if env.Name == envName {
			return true
		}
	}
	return false
}

func AddServiceAddress(params map[string]string, nodeType rayiov1alpha1.RayNodeType, svcName string) (completeStartParams map[string]string) {
	if nodeType == rayiov1alpha1.WorkerNode {
		if _, ok := params["address"]; !ok {
			var svcPort string
			if _, ok := params["port"]; !ok {
				svcPort = string(common.DefaultRedisPort)
			} else {
				svcPort = params["port"]
			}
			params["address"] = fmt.Sprintf("%s:%s", svcName, svcPort)
		}
	}
	return params
}

// BuildCommandFromParams build ray start command for head and worker node group.
func BuildCommandFromParams(nodeType rayiov1alpha1.RayNodeType, params map[string]string) (fullCmd string) {
	pairs := createKeyValuePairs(params)

	switch nodeType {
	case rayiov1alpha1.HeadNode:
		return fmt.Sprintf("ulimit -n 65536; ray start --head %s", pairs)
	case rayiov1alpha1.WorkerNode:
		return fmt.Sprintf("ulimit -n 65536; ray start --block %s", pairs)
	default:
		logrus.Error(fmt.Errorf("missing or unknown node type"), "node type (head or worker) should be given")
	}
	return ""
}

// https://stackoverflow.com/a/48150584
func createKeyValuePairs(params map[string]string) (s string) {
	flags := new(bytes.Buffer)
	for k, v := range params {
		fmt.Fprintf(flags, " --%s=%s ", k, v)
	}
	return flags.String()
}
