package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

// NodeLabelsWithheldReason is the reason of the Warning event recorded on a pod whose node labels were not delivered
const NodeLabelsWithheldReason = "RequiredNodeLabelMissing"

var podBindingLog = logf.Log.WithName("pod-binding-webhook")

//+kubebuilder:webhook:path=/mutate-v1-pod-binding,mutating=true,failurePolicy=ignore,sideEffects=NoneOnDryRun,groups="",resources=pods/binding,verbs=create,versions=v1,name=mpodbinding.kb.io,admissionReviewVersions=v1,timeoutSeconds=5
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get

// SetupPodBindingWebhookWithManager registers the pods/binding CREATE mutating webhook, which writes the bound node's
// mapped labels into the Binding annotations. The API server copies them onto the pod together with spec.nodeName.
func SetupPodBindingWebhookWithManager(mgr ctrl.Manager, config configapi.Configuration) error {
	mgr.GetWebhookServer().Register("/mutate-v1-pod-binding", &webhook.Admission{Handler: &PodBindingWebhook{
		Client:            mgr.GetClient(),
		APIReader:         mgr.GetAPIReader(),
		Recorder:          mgr.GetEventRecorder("kuberay-pod-binding-webhook"),
		Decoder:           admission.NewDecoder(mgr.GetScheme()),
		AllowedNodeLabels: sets.New(config.AllowedNodeLabels...),
	}})
	return nil
}

type PodBindingWebhook struct {
	// Client reads pods and RayClusters from the manager cache
	Client client.Reader
	// APIReader reads pods the cache does not hold and node metadata from the API server
	APIReader client.Reader
	// Recorder emits the Warning event when labels are withheld, may be nil
	Recorder events.EventRecorder
	Decoder  admission.Decoder
	// AllowedNodeLabels is the operator allowlist for topology.labelMappings
	AllowedNodeLabels sets.Set[string]
}

// Handle implements admission.Handler
func (w *PodBindingWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	if !features.Enabled(features.TopologyLabelDelivery) {
		return admission.Allowed("TopologyLabelDelivery feature gate is disabled")
	}
	binding := &corev1.Binding{}
	if err := w.Decoder.Decode(req, binding); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if _, done := binding.Annotations[utils.RayTopologyLabelsAnnotationKey]; done {
		// already delivered e.g. on a webhook reinvocation
		return admission.Allowed("node labels already set on the binding")
	}
	nodeName := binding.Target.Name
	podName := req.Name
	if nodeName == "" {
		return admission.Allowed("binding has no pod or node name")
	}
	log := podBindingLog.WithValues("pod", req.Namespace+"/"+podName, "node", nodeName)

	podMeta, err := w.getPodMetadata(ctx, types.NamespacedName{Namespace: req.Namespace, Name: podName})
	if err != nil {
		// a prepared pod exits at container start without the annotation, so admitting is still loud
		log.Error(err, "cannot read pod, admitting the binding without node labels")
		return admission.Allowed("pod lookup failed")
	}
	if podMeta == nil || podMeta.Labels[utils.RayClusterLabelKey] == "" || podMeta.Labels[utils.RayNodeTypeLabelKey] != string(rayv1.WorkerNode) {
		return admission.Allowed("not a Ray worker pod")
	}

	labels, err := w.resolveNodeLabels(ctx, req.Namespace, podMeta, nodeName)
	if err != nil {
		w.withhold(req, podMeta, nodeName, err)
		return admission.Allowed("node labels withheld")
	}
	if labels == nil {
		return admission.Allowed("worker group does not set topology")
	}
	encoded, err := json.Marshal(labels)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if binding.Annotations == nil {
		binding.Annotations = map[string]string{}
	}
	binding.Annotations[utils.RayTopologyLabelsAnnotationKey] = string(encoded)
	patched, err := json.Marshal(binding)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	log.Info("delivering node labels", "labels", labels)
	return admission.PatchResponseFromRaw(req.Object.Raw, patched)
}

// resolveNodeLabels returns the Ray labels to deliver, nil when the worker group has no label mappings
func (w *PodBindingWebhook) resolveNodeLabels(ctx context.Context, namespace string, podMeta *metav1.ObjectMeta, nodeName string) (map[string]string, error) {
	clusterName := podMeta.Labels[utils.RayClusterLabelKey]
	groupName := podMeta.Labels[utils.RayNodeGroupLabelKey]
	cluster := &rayv1.RayCluster{}
	if err := w.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clusterName}, cluster); err != nil {
		return nil, fmt.Errorf("cannot read RayCluster %s: %w", clusterName, err)
	}
	group := findWorkerGroupSpec(&cluster.Spec, groupName)
	if group == nil {
		return nil, fmt.Errorf("worker group %s not found in RayCluster %s", groupName, clusterName)
	}
	if group.Topology == nil || len(group.Topology.LabelMappings) == 0 {
		return nil, nil
	}
	node := &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Node"}}
	if err := w.APIReader.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		return nil, fmt.Errorf("cannot read node %s: %w", nodeName, err)
	}
	return buildTopologyLabels(node.Labels, group.Topology.LabelMappings, w.AllowedNodeLabels)
}

// buildTopologyLabels maps node labels to Ray labels following the mappings. Every mapping must resolve to a
// non-empty allowlisted node label, otherwise the whole set fails
func buildTopologyLabels(nodeLabels map[string]string, mappings []rayv1.TopologyLabelMapping, allowed sets.Set[string]) (map[string]string, error) {
	labels := make(map[string]string, len(mappings))
	var errList []string
	for _, mapping := range mappings {
		value, ok := nodeLabels[mapping.NodeLabel]
		switch {
		case !allowed.Has(mapping.NodeLabel):
			errList = append(errList, fmt.Sprintf("node label %q is not in the operator's allowedNodeLabels", mapping.NodeLabel))
		case !ok:
			errList = append(errList, fmt.Sprintf("node lacks label %q", mapping.NodeLabel))
		case value == "":
			errList = append(errList, fmt.Sprintf("node label %q is empty", mapping.NodeLabel))
		default:
			// an empty mapTo delivers under the node label key
			key := mapping.MapTo
			if key == "" {
				key = mapping.NodeLabel
			}
			labels[key] = value
		}
	}
	if len(errList) > 0 {
		return nil, errors.New(strings.Join(errList, "; "))
	}
	return labels, nil
}

// getPodMetadata returns the pod metadata, or nil when the pod does not exist. The cache only holds Ray node pods,
// so a miss falls back to a metadata-only GET
func (w *PodBindingWebhook) getPodMetadata(ctx context.Context, key types.NamespacedName) (*metav1.ObjectMeta, error) {
	pod := &corev1.Pod{}
	err := w.Client.Get(ctx, key, pod)
	if err == nil {
		return &pod.ObjectMeta, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	podMeta := &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}}
	if err := w.APIReader.Get(ctx, key, podMeta); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &podMeta.ObjectMeta, nil
}

// withhold records why the labels were not delivered. The binding proceeds without the annotation and the pod exits before ray start
func (w *PodBindingWebhook) withhold(req admission.Request, podMeta *metav1.ObjectMeta, nodeName string, cause error) {
	message := fmt.Sprintf("node labels not delivered for the binding to node %s: %v", nodeName, cause)
	podBindingLog.Info(message, "pod", podMeta.Namespace+"/"+podMeta.Name)
	if w.Recorder == nil || (req.DryRun != nil && *req.DryRun) {
		return
	}
	pod := &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: *podMeta,
	}
	w.Recorder.Eventf(pod, nil, corev1.EventTypeWarning, NodeLabelsWithheldReason, "DeliverNodeLabels", "%s", message)
}
