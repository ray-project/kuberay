package v1

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

const (
	testZoneLabel   = "topology.kubernetes.io/zone"
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
				{GroupName: "test", Topology: &rayv1.TopologySpec{LabelMappings: []rayv1.TopologyLabelMapping{{NodeLabel: testZoneLabel}}}},
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

// TestRayStartWithNodeLabelsShell runs the generated shell fragment with `ray start` replaced by `echo`
// 3 runs: no labels, empty label value, good label value
func TestRayStartWithNodeLabelsShell(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	t.Cleanup(func() { _ = os.Remove(utils.RayTopologyLabelsFilePath) })

	script := rayStartWithNodeLabels("echo RAYSTART")
	run := func(labelsJSON *string) (string, error) {
		cmd := exec.CommandContext(t.Context(), bash, "-c", "--", script)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		if labelsJSON != nil {
			cmd.Env = append(cmd.Env, utils.RAY_NODE_LABELS_JSON+"="+*labelsJSON)
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run(nil)
	require.Error(t, err, "missing labels must fail before ray start")
	assert.Contains(t, out, "is empty")

	empty := `{"ray.io/zone":""}`
	out, err = run(&empty)
	require.Error(t, err, "an empty label value must fail before ray start")
	assert.Contains(t, out, "empty label value")

	good := `{"ray.io/zone":"us-central1-a"}`
	out, err = run(&good)
	require.NoError(t, err, out)
	assert.Contains(t, out, "RAYSTART --labels-file="+utils.RayTopologyLabelsFilePath)
	content, err := os.ReadFile(utils.RayTopologyLabelsFilePath)
	require.NoError(t, err)
	assert.Equal(t, good+"\n", string(content))
}

func TestPrepareNodeLabelDelivery(t *testing.T) {
	pod := newWorkerPod("test")
	require.NoError(t, prepareNodeLabelDelivery(pod))
	container := pod.Spec.Containers[utils.RayContainerIndex]
	pointer := nodeLabelsEnv(container.Env)
	require.NotNil(t, pointer)
	assert.Equal(t, "metadata.annotations['"+utils.RayTopologyLabelsAnnotationKey+"']", pointer.ValueFrom.FieldRef.FieldPath)
	assert.Equal(t, testUlimitCmd+rayStartWithNodeLabels(testRayStartCmd), container.Args[0])

	t.Run("reinvocation leaves the pod unchanged", func(t *testing.T) {
		before := pod.DeepCopy()
		require.NoError(t, prepareNodeLabelDelivery(pod))
		assert.Equal(t, before, pod)
	})

	refused := []struct {
		name   string
		mutate func(*corev1.PodSpec)
		err    string
	}{
		{"no containers", func(s *corev1.PodSpec) { s.Containers = nil }, "no Ray container"},
		{"user-set pointer", func(s *corev1.PodSpec) {
			s.Containers[0].Env = append(s.Containers[0].Env, corev1.EnvVar{Name: utils.RAY_NODE_LABELS_JSON, Value: "{}"})
		}, "managed by KubeRay"},
		{"missing generated command", func(s *corev1.PodSpec) { s.Containers[0].Env = nil }, utils.KUBERAY_GEN_RAY_START_CMD},
		{"generated command missing from args", func(s *corev1.PodSpec) { s.Containers[0].Args = []string{"echo no ray here"} }, "was not found"},
		{"generated command twice in args", func(s *corev1.PodSpec) {
			s.Containers[0].Args = []string{testRayStartCmd + " && " + testRayStartCmd}
		}, "appears 2 times"},
	}
	for _, tc := range refused {
		t.Run(tc.name+" is refused", func(t *testing.T) {
			pod := newWorkerPod("test")
			tc.mutate(&pod.Spec)
			require.ErrorContains(t, prepareNodeLabelDelivery(pod), tc.err)
		})
	}
}

func TestPodWebhookDefault(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.TopologyLabelDelivery, true)
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, rayv1.AddToScheme(scheme))
	webhook := &PodWebhook{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(newTopologyCluster()).Build()}

	unchanged := func(t *testing.T, pod *corev1.Pod) {
		before := pod.DeepCopy()
		require.NoError(t, webhook.Default(ctx, pod))
		assert.Equal(t, before, pod)
	}

	t.Run("disabled feature gate leaves the pod alone", func(t *testing.T) {
		features.SetFeatureGateDuringTest(t, features.TopologyLabelDelivery, false)
		unchanged(t, newWorkerPod("test"))
	})

	t.Run("head pod is left alone", func(t *testing.T) {
		pod := newWorkerPod("test")
		pod.Labels[utils.RayNodeTypeLabelKey] = string(rayv1.HeadNode)
		unchanged(t, pod)
	})

	t.Run("worker of a group without label mappings is left alone", func(t *testing.T) {
		unchanged(t, newWorkerPod("plain"))
		unchanged(t, newWorkerPod("empty"))
	})

	t.Run("worker of a topology group is prepared", func(t *testing.T) {
		pod := newWorkerPod("test")
		require.NoError(t, webhook.Default(ctx, pod))
		assert.NotNil(t, nodeLabelsEnv(pod.Spec.Containers[utils.RayContainerIndex].Env))
	})

	t.Run("unknown RayCluster refuses the pod", func(t *testing.T) {
		pod := newWorkerPod("test")
		pod.Labels[utils.RayClusterLabelKey] = "missing"
		require.ErrorContains(t, webhook.Default(ctx, pod), "cannot read RayCluster default/missing")
	})

	t.Run("unknown worker group refuses the pod", func(t *testing.T) {
		require.ErrorContains(t, webhook.Default(ctx, newWorkerPod("ghost")), "worker group ghost not found")
	})
}
