package v1

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

func TestBuildTopologyLabels(t *testing.T) {
	allowed := sets.New("topology.kubernetes.io/zone", "nvidia.com/gpu.clique")
	mappings := []rayv1.TopologyLabelMapping{{NodeLabel: "topology.kubernetes.io/zone"}, {NodeLabel: "nvidia.com/gpu.clique", MapTo: "ray.io/accelerator-domain"}}
	nodeLabels := map[string]string{"topology.kubernetes.io/zone": "us-central1-a", "nvidia.com/gpu.clique": "abc.0", "unrelated": "x"}

	labels, err := buildTopologyLabels(nodeLabels, mappings, allowed)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"topology.kubernetes.io/zone": "us-central1-a", "ray.io/accelerator-domain": "abc.0"}, labels)

	_, err = buildTopologyLabels(map[string]string{"topology.kubernetes.io/zone": "us-central1-a"}, mappings, allowed)
	require.ErrorContains(t, err, `node lacks label "nvidia.com/gpu.clique"`)

	_, err = buildTopologyLabels(map[string]string{"topology.kubernetes.io/zone": "", "nvidia.com/gpu.clique": "abc.0"}, mappings, allowed)
	require.ErrorContains(t, err, `node label "topology.kubernetes.io/zone" is empty`)

	_, err = buildTopologyLabels(nodeLabels, mappings, sets.New("topology.kubernetes.io/zone"))
	require.ErrorContains(t, err, `node label "nvidia.com/gpu.clique" is not in the operator's allowedNodeLabels`)
}

func newBinding(podName, nodeName string) *corev1.Binding {
	return &corev1.Binding{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: "default"},
		Target:     corev1.ObjectReference{Kind: "Node", Name: nodeName},
	}
}

func newBindingRequest(t *testing.T, binding *corev1.Binding, dryRun bool) admission.Request {
	binding.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Binding"}
	raw, err := json.Marshal(binding)
	require.NoError(t, err)
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation:   admissionv1.Create,
		Name:        binding.Name,
		Namespace:   binding.Namespace,
		Kind:        metav1.GroupVersionKind{Version: "v1", Kind: "Binding"},
		Resource:    metav1.GroupVersionResource{Version: "v1", Resource: "pods"},
		SubResource: "binding",
		Object:      runtime.RawExtension{Raw: raw},
		DryRun:      &dryRun,
	}}
}

// deliveredLabels returns the labels the response writes into the Binding annotation, or nil
func deliveredLabels(t *testing.T, resp admission.Response) map[string]string {
	for _, patch := range resp.Patches {
		if patch.Path != "/metadata/annotations" {
			continue
		}
		encoded, ok := patch.Value.(map[string]any)[utils.RayTopologyLabelsAnnotationKey].(string)
		require.True(t, ok, "annotations patch should carry the labels annotation")
		labels := map[string]string{}
		require.NoError(t, json.Unmarshal([]byte(encoded), &labels))
		return labels
	}
	return nil
}

func drainEvents(recorder *events.FakeRecorder) []string {
	var drained []string
	for {
		select {
		case event := <-recorder.Events:
			drained = append(drained, event)
		default:
			return drained
		}
	}
}

func TestPodBindingWebhookHandle(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.TopologyLabelDelivery, true)
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, rayv1.AddToScheme(scheme))
	topoPod := newWorkerPod("test")
	plainPod := newWorkerPod("plain")
	otherPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: "default"}}
	zoneNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"topology.kubernetes.io/zone": "us-central1-a"}}}
	bareNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(newTopologyCluster(), topoPod, plainPod, otherPod, zoneNode, bareNode).Build()

	newHandler := func(allowed ...string) (*PodBindingWebhook, *events.FakeRecorder) {
		recorder := events.NewFakeRecorder(10)
		return &PodBindingWebhook{
			Client:            c,
			APIReader:         c,
			Recorder:          recorder,
			Decoder:           admission.NewDecoder(scheme),
			AllowedNodeLabels: sets.New(allowed...),
		}, recorder
	}
	passThrough := func(t *testing.T, resp admission.Response, recorder *events.FakeRecorder) {
		require.True(t, resp.Allowed, resp.Result)
		assert.Empty(t, resp.Patches)
		assert.Empty(t, drainEvents(recorder))
	}
	withheld := func(t *testing.T, resp admission.Response, recorder *events.FakeRecorder, cause string) {
		require.True(t, resp.Allowed, resp.Result)
		assert.Nil(t, deliveredLabels(t, resp))
		recorded := drainEvents(recorder)
		require.Len(t, recorded, 1)
		assert.Contains(t, recorded[0], NodeLabelsWithheldReason)
		assert.Contains(t, recorded[0], cause)
	}

	t.Run("delivers the mapped labels on the binding", func(t *testing.T) {
		handler, recorder := newHandler("topology.kubernetes.io/zone")
		resp := handler.Handle(ctx, newBindingRequest(t, newBinding(topoPod.Name, "node-a"), false))
		require.True(t, resp.Allowed, resp.Result)
		assert.Equal(t, map[string]string{"topology.kubernetes.io/zone": "us-central1-a"}, deliveredLabels(t, resp))
		assert.Empty(t, drainEvents(recorder))
	})

	t.Run("withholds and records an event when the node lacks a label", func(t *testing.T) {
		handler, recorder := newHandler("topology.kubernetes.io/zone")
		withheld(t, handler.Handle(ctx, newBindingRequest(t, newBinding(topoPod.Name, "node-b"), false)), recorder, `node lacks label "topology.kubernetes.io/zone"`)
	})

	t.Run("withholds when a mapped label is not allowlisted", func(t *testing.T) {
		handler, recorder := newHandler()
		withheld(t, handler.Handle(ctx, newBindingRequest(t, newBinding(topoPod.Name, "node-a"), false)), recorder, "not in the operator's allowedNodeLabels")
	})

	t.Run("dry run records no event", func(t *testing.T) {
		handler, recorder := newHandler()
		passThrough(t, handler.Handle(ctx, newBindingRequest(t, newBinding(topoPod.Name, "node-b"), true)), recorder)
	})

	t.Run("disabled feature gate passes through", func(t *testing.T) {
		features.SetFeatureGateDuringTest(t, features.TopologyLabelDelivery, false)
		handler, recorder := newHandler()
		passThrough(t, handler.Handle(ctx, newBindingRequest(t, newBinding(topoPod.Name, "node-a"), false)), recorder)
	})

	t.Run("passes through a group without label mappings", func(t *testing.T) {
		handler, recorder := newHandler()
		passThrough(t, handler.Handle(ctx, newBindingRequest(t, newBinding(plainPod.Name, "node-a"), false)), recorder)
	})

	t.Run("passes through non-Ray and unknown pods", func(t *testing.T) {
		handler, recorder := newHandler()
		passThrough(t, handler.Handle(ctx, newBindingRequest(t, newBinding(otherPod.Name, "node-a"), false)), recorder)
		passThrough(t, handler.Handle(ctx, newBindingRequest(t, newBinding("ghost", "node-a"), false)), recorder)
	})

	t.Run("passes through a binding that already carries labels", func(t *testing.T) {
		handler, recorder := newHandler()
		binding := newBinding(topoPod.Name, "node-a")
		binding.Annotations = map[string]string{utils.RayTopologyLabelsAnnotationKey: "{}"}
		passThrough(t, handler.Handle(ctx, newBindingRequest(t, binding, false)), recorder)
	})
}
