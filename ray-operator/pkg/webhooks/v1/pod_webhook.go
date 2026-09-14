package v1

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

// PodMutatingWebhookPath is the path of the pods CREATE mutating webhook.
const PodMutatingWebhookPath = "/mutate-v1-pod"

var podLog = logf.Log.WithName("pod-webhook")

//+kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

// SetupPodWebhookWithManager registers the pods CREATE mutating webhook with the manager. The webhook mutates Ray worker pods to prepare them for node label delivery.
func SetupPodWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register(PodMutatingWebhookPath, admission.WithDefaulter(mgr.GetScheme(), &PodWebhook{
		Client: mgr.GetClient(),
	}))
	return nil
}

type PodWebhook struct {
	// Client reads RayClusters from the manager cache
	Client client.Reader
}

// default implements admission.Defaulter. controller-runtime turns the in-place changes into a JSON patch
func (w *PodWebhook) Default(ctx context.Context, pod *corev1.Pod) error {
	if !features.Enabled(features.TopologyLabelDelivery) {
		return nil
	}
	clusterName := pod.Labels[utils.RayClusterLabelKey]
	if clusterName == "" || pod.Labels[utils.RayNodeTypeLabelKey] != string(rayv1.WorkerNode) {
		return nil
	}
	namespace := pod.Namespace
	if namespace == "" {
		if req, err := admission.RequestFromContext(ctx); err == nil {
			namespace = req.Namespace
		}
	}
	groupName := pod.Labels[utils.RayNodeGroupLabelKey]

	cluster := &rayv1.RayCluster{}
	if err := w.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clusterName}, cluster); err != nil {
		return fmt.Errorf("cannot read RayCluster %s/%s for a worker pod of group %s: %w", namespace, clusterName, groupName, err)
	}
	i := slices.IndexFunc(cluster.Spec.WorkerGroupSpecs, func(g rayv1.WorkerGroupSpec) bool { return g.GroupName == groupName })
	if i < 0 {
		return fmt.Errorf("worker group %s not found in RayCluster %s/%s", groupName, namespace, clusterName)
	}
	group := &cluster.Spec.WorkerGroupSpecs[i]

	// TODO: vendor-specific(TPU, etc.) CREATE-time mutations

	if group.Topology == nil || len(group.Topology.LabelMappings) == 0 {
		return nil
	}
	if err := prepareNodeLabelDelivery(pod); err != nil {
		return fmt.Errorf("worker pod of group %s in RayCluster %s/%s: %w", groupName, namespace, clusterName, err)
	}
	podLog.Info("prepared pod for node label delivery", "namespace", namespace, "rayCluster", clusterName,
		"group", groupName, "labelMappings", len(group.Topology.LabelMappings))
	return nil
}

// adds the RAY_NODE_LABELS_JSON downward API env var and rewrites the ray start args to load it with --labels-file
func prepareNodeLabelDelivery(pod *corev1.Pod) error {
	if len(pod.Spec.Containers) <= utils.RayContainerIndex {
		return fmt.Errorf("pod has no Ray container")
	}
	container := &pod.Spec.Containers[utils.RayContainerIndex]

	pointer := corev1.EnvVar{
		Name: utils.RAY_NODE_LABELS_JSON,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: fmt.Sprintf("metadata.annotations['%s']", utils.RayTopologyLabelsAnnotationKey),
			},
		},
	}
	for _, env := range container.Env {
		if env.Name != pointer.Name {
			continue
		}
		if env.ValueFrom != nil && env.ValueFrom.FieldRef != nil && env.ValueFrom.FieldRef.FieldPath == pointer.ValueFrom.FieldRef.FieldPath {
			// pointer already exists e.g. on a webhook reinvocation
			return nil
		}
		return fmt.Errorf("%s is managed by KubeRay when topology is set and must not be set in the pod template", pointer.Name)
	}
	i := slices.IndexFunc(container.Env, func(e corev1.EnvVar) bool { return e.Name == utils.KUBERAY_GEN_RAY_START_CMD })
	if i < 0 || container.Env[i].Value == "" {
		return fmt.Errorf("the Ray container has no %s env var; node label delivery needs the KubeRay-generated ray start command", utils.KUBERAY_GEN_RAY_START_CMD)
	}
	rayStartCmd := container.Env[i].Value
	if len(container.Args) != 1 {
		return fmt.Errorf("expected the Ray container to have exactly one args element carrying the generated ray start command, got %d", len(container.Args))
	}
	rewritten, err := rewriteRayStartArgs(container.Args[0], rayStartCmd)
	if err != nil {
		return err
	}
	container.Args[0] = rewritten
	container.Env = append(container.Env, pointer)
	return nil
}

func rewriteRayStartArgs(args, rayStartCmd string) (string, error) {
	switch n := strings.Count(args, rayStartCmd); {
	case n == 0:
		return "", fmt.Errorf("the generated ray start command (%s) was not found in the container args; node label delivery needs the KubeRay-generated ray start command", utils.KUBERAY_GEN_RAY_START_CMD)
	case n > 1:
		return "", fmt.Errorf("the generated ray start command appears %d times in the container args", n)
	}
	return strings.Replace(args, rayStartCmd, rayStartWithNodeLabels(rayStartCmd), 1), nil
}

// returns the shell command that 1) writes RAY_NODE_LABELS_JSON to the labels file 2) runs rayStartCmd with --labels-file
func rayStartWithNodeLabels(rayStartCmd string) string {
	return fmt.Sprintf(
		`if [ -z "$%[1]s" ]; then echo "%[1]s is empty: node labels were not delivered to this pod, see the pod events" >&2; exit 1; fi; `+
			`case "$%[1]s" in *'":""'*) echo "%[1]s contains an empty label value" >&2; exit 1;; esac; `+
			`printf '%%s\n' "$%[1]s" > %[2]s; %[3]s --labels-file=%[2]s`,
		utils.RAY_NODE_LABELS_JSON, utils.RayTopologyLabelsFilePath, rayStartCmd)
}
