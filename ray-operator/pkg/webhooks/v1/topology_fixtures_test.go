package v1

// shared fixtures for the topology validation, pods and pods/binding webhook tests

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	testRayStartCmd = "ray start --block --address=topo-head-svc.default.svc.cluster.local:6379 --num-cpus=1"
	testUlimitCmd   = "ulimit -n ${RAY_START_ULIMIT_OPEN_FILES:-65536}; "
)

// newTopologyCluster returns a RayCluster with a plain worker group, one with empty topology and a topology-enabled one
func newTopologyCluster() *rayv1.RayCluster {
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "topo", Namespace: "default"},
		Spec: rayv1.RayClusterSpec{
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{GroupName: "plain"},
				{GroupName: "empty", Topology: &rayv1.TopologySpec{}},
				{GroupName: "test", Topology: &rayv1.TopologySpec{LabelMappings: []rayv1.TopologyLabelMapping{{NodeLabel: "topology.kubernetes.io/zone"}}}},
			},
		},
	}
}

// newWorkerPod returns a pod shaped like BuildPod's output for a worker of the given group
func newWorkerPod(group string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "topo-" + group + "-worker-abcde",
			Namespace: "default",
			Labels: map[string]string{
				utils.RayClusterLabelKey:   "topo",
				utils.RayNodeTypeLabelKey:  string(rayv1.WorkerNode),
				utils.RayNodeGroupLabelKey: group,
				utils.RayNodeLabelKey:      "yes",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:    "ray-worker",
			Image:   "rayproject/ray:2.45.0",
			Command: []string{"/bin/bash", "-c", "--"},
			Args:    []string{testUlimitCmd + testRayStartCmd},
			Env:     []corev1.EnvVar{{Name: utils.KUBERAY_GEN_RAY_START_CMD, Value: testRayStartCmd}},
		}}},
	}
}

// nodeLabelsEnv returns the RAY_NODE_LABELS_JSON env var, or nil
func nodeLabelsEnv(envs []corev1.EnvVar) *corev1.EnvVar {
	if i := slices.IndexFunc(envs, func(e corev1.EnvVar) bool { return e.Name == utils.RAY_NODE_LABELS_JSON }); i >= 0 {
		return &envs[i]
	}
	return nil
}

// newTopologyRayCluster returns a randomly named RayCluster with one topology worker group carrying mapping
func newTopologyRayCluster(mapping rayv1.TopologyLabelMapping) *rayv1.RayCluster {
	template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ray"}}}}
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("topo-%d", rand.IntnRange(1000, 9000)), Namespace: "default"},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{Template: template},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{{
				GroupName:   "test",
				MinReplicas: new(int32(1)),
				MaxReplicas: new(int32(1)),
				Template:    template,
				Topology:    &rayv1.TopologySpec{LabelMappings: []rayv1.TopologyLabelMapping{mapping}},
			}},
		},
	}
}
