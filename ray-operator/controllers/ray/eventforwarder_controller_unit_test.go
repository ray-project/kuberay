package ray

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func TestEventForwarder_OneEventPerClusterAcrossPods(t *testing.T) {
	// Two Pods of cluster-a and one Pod of cluster-b all on node-1 => two Events.
	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b", Namespace: "default"}},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		rayPodOnNode("a-worker", "default", "cluster-a", "node-1"),
		rayPodOnNode("b-head", "default", "cluster-b", "node-1"),
		warningNodeEvent("evt-1", "node-1"),
	)

	reconcileForwarderEvent(t, r, "evt-1")

	assert.ElementsMatch(t,
		[]string{"RayCluster/default/cluster-a", "RayCluster/default/cluster-b"},
		recorder.targets(t))
	for _, e := range recorder.events {
		assert.Equal(t, corev1.EventTypeWarning, e.eventtype)
		assert.Equal(t, forwardedEventReason, e.reason)
		assert.Contains(t, e.message, `Node "node-1"`)
		assert.Contains(t, e.message, "XIDError")
		assert.Contains(t, e.message, "node-problem-detector")
		assert.Contains(t, e.message, "Caught XID error, XID=79")
	}
}

func TestEventForwarder_ForwardsToOwningRayJob(t *testing.T) {
	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "job-a-raycluster", Namespace: "default"}},
		&rayv1.RayJob{ObjectMeta: metav1.ObjectMeta{Name: "job-a", Namespace: "default"}},
		rayJobPodOnNode("a-worker", "default", "job-a-raycluster", "job-a", "node-1"),
		warningNodeEvent("evt-1", "node-1"),
	)

	reconcileForwarderEvent(t, r, "evt-1")

	assert.ElementsMatch(t,
		[]string{"RayCluster/default/job-a-raycluster", "RayJob/default/job-a"},
		recorder.targets(t))
}

func TestEventForwarder_SkipsNodesWithoutRayPods(t *testing.T) {
	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		warningNodeEvent("evt-2", "node-2"), // event for a node with no Ray pods
	)

	reconcileForwarderEvent(t, r, "evt-2")

	assert.Empty(t, recorder.events)
}

func TestEventForwarder_ResyncDoesNotReforward(t *testing.T) {
	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		warningNodeEvent("evt-1", "node-1"),
	)

	reconcileForwarderEvent(t, r, "evt-1")
	reconcileForwarderEvent(t, r, "evt-1") // resync/requeue of the unchanged Event

	assert.Len(t, recorder.events, 1, "an unchanged source Event must be forwarded only once")
}

func TestEventForwarder_CountBumpReforwards(t *testing.T) {
	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		warningNodeEvent("evt-1", "node-1"),
	)
	ctx := context.Background()

	reconcileForwarderEvent(t, r, "evt-1")

	// The fault recurs: Kubernetes aggregates it into the same Event object by
	// bumping count and lastTimestamp.
	evt := &corev1.Event{}
	require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "evt-1", Namespace: "default"}, evt))
	evt.Count = 3
	evt.LastTimestamp = metav1.NewTime(time.Now())
	require.NoError(t, r.Update(ctx, evt))

	reconcileForwarderEvent(t, r, "evt-1")
	reconcileForwarderEvent(t, r, "evt-1") // no further occurrences

	assert.Len(t, recorder.events, 2, "a recurrence (count bump) must be forwarded again")
}

func TestEventForwarder_NewUIDUnderSameNameForwards(t *testing.T) {
	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		warningNodeEvent("evt-1", "node-1"),
	)
	ctx := context.Background()

	reconcileForwarderEvent(t, r, "evt-1")

	// The Event expires and a new one is created under the same name.
	evt := &corev1.Event{}
	require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "evt-1", Namespace: "default"}, evt))
	require.NoError(t, r.Delete(ctx, evt))
	replacement := warningNodeEvent("evt-1", "node-1")
	replacement.UID = "evt-1-uid-2"
	require.NoError(t, r.Create(ctx, replacement))

	reconcileForwarderEvent(t, r, "evt-1")

	assert.Len(t, recorder.events, 2, "a new source Event reusing the name must be forwarded")
}

func TestEventForwarder_ForgetsDeletedEvents(t *testing.T) {
	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		warningNodeEvent("evt-1", "node-1"),
	)
	ctx := context.Background()
	key := types.NamespacedName{Name: "evt-1", Namespace: "default"}

	reconcileForwarderEvent(t, r, "evt-1")
	assert.Contains(t, r.forwarded, key)

	evt := &corev1.Event{}
	require.NoError(t, r.Get(ctx, key, evt))
	require.NoError(t, r.Delete(ctx, evt))
	reconcileForwarderEvent(t, r, "evt-1") // delete notification

	assert.NotContains(t, r.forwarded, key, "tracking entries of expired Events must be dropped")
}

func TestEventForwarder_Filters(t *testing.T) {
	tests := []struct {
		mutate  func(*corev1.Event)
		name    string
		options EventForwarderOptions
		want    int
	}{
		{
			name:   "normal events are dropped by default",
			mutate: func(e *corev1.Event) { e.Type = corev1.EventTypeNormal },
			want:   0,
		},
		{
			name:    "normal events forwarded when configured",
			options: EventForwarderOptions{Types: []string{corev1.EventTypeNormal, corev1.EventTypeWarning}},
			mutate:  func(e *corev1.Event) { e.Type = corev1.EventTypeNormal },
			want:    1,
		},
		{
			name:    "matching source component",
			options: EventForwarderOptions{Sources: []string{"node-problem-detector"}},
			mutate:  func(_ *corev1.Event) {},
			want:    1,
		},
		{
			name:    "matching reportingController",
			options: EventForwarderOptions{Sources: []string{"nvidia-gpu-device-plugin"}},
			mutate: func(e *corev1.Event) {
				e.Source = corev1.EventSource{}
				e.ReportingController = "nvidia-gpu-device-plugin"
			},
			want: 1,
		},
		{
			name:    "non-matching source",
			options: EventForwarderOptions{Sources: []string{"nvidia-gpu-device-plugin"}},
			mutate:  func(_ *corev1.Event) {},
			want:    0,
		},
		{
			name:    "matching reason",
			options: EventForwarderOptions{Reasons: []string{"XIDError", "KernelDeadlock"}},
			mutate:  func(_ *corev1.Event) {},
			want:    1,
		},
		{
			name:    "non-matching reason",
			options: EventForwarderOptions{Reasons: []string{"KernelDeadlock"}},
			mutate:  func(_ *corev1.Event) {},
			want:    0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evt := warningNodeEvent("evt-1", "node-1")
			tc.mutate(evt)
			recorder := &capturingRecorder{}
			r := newEventForwarder(t, recorder, tc.options,
				&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
				rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
				evt,
			)

			reconcileForwarderEvent(t, r, "evt-1")

			assert.Len(t, recorder.events, tc.want)
		})
	}
}

func TestEventForwarder_SkipsEventsObservedBeforeStart(t *testing.T) {
	legacyOld := warningNodeEvent("evt-legacy", "node-1")
	legacyOld.LastTimestamp = metav1.NewTime(time.Now().Add(-2 * time.Hour))

	// New-style events carry eventTime instead of lastTimestamp.
	newStyleOld := warningNodeEvent("evt-new", "node-1")
	newStyleOld.LastTimestamp = metav1.Time{}
	newStyleOld.EventTime = metav1.NewMicroTime(time.Now().Add(-2 * time.Hour))

	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		legacyOld, newStyleOld,
	)
	// startedAt is now; both events predate it and are informer replays.
	r.startedAt = time.Now()

	reconcileForwarderEvent(t, r, "evt-legacy")
	reconcileForwarderEvent(t, r, "evt-new")

	assert.Empty(t, recorder.events, "events observed before start must be ignored")
}

func TestEventForwarder_RecurrenceOfOldEventForwards(t *testing.T) {
	// The Event object predates the controller, but the fault recurs after
	// startup: series.lastObservedTime moves forward and it must be forwarded.
	evt := warningNodeEvent("evt-1", "node-1")
	evt.LastTimestamp = metav1.NewTime(time.Now().Add(-2 * time.Hour))
	evt.Series = &corev1.EventSeries{Count: 5, LastObservedTime: metav1.NewMicroTime(time.Now().Add(time.Minute))}

	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		evt,
	)
	r.startedAt = time.Now()

	reconcileForwarderEvent(t, r, "evt-1")

	assert.Len(t, recorder.events, 1)
}

func TestEventForwarder_IgnoresNonNodeEvents(t *testing.T) {
	podEvent := warningNodeEvent("evt-pod", "a-head")
	podEvent.InvolvedObject = corev1.ObjectReference{Kind: "Pod", Name: "a-head", Namespace: "default"}

	recorder := &capturingRecorder{}
	r := newEventForwarder(t, recorder, EventForwarderOptions{},
		&rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"}},
		rayPodOnNode("a-head", "default", "cluster-a", "node-1"),
		podEvent,
	)

	reconcileForwarderEvent(t, r, "evt-pod")

	assert.Empty(t, recorder.events)
}

// capturedEvent records one emission through the capturingRecorder.
type capturedEvent struct {
	object    runtime.Object
	related   runtime.Object
	eventtype string
	reason    string
	action    string
	message   string
}

// capturingRecorder implements record.EventRecorder and captures the target
// object and annotations, which record.FakeRecorder discards.
type capturingRecorder struct {
	events []capturedEvent
}

func (c *capturingRecorder) Eventf(regarding runtime.Object, related runtime.Object, eventtype, reason, action, note string, args ...any) {
	c.events = append(c.events, capturedEvent{
		object:    regarding,
		related:   related,
		eventtype: eventtype,
		reason:    reason,
		action:    action,
		message:   fmt.Sprintf(note, args...),
	})
}

// targets renders the involved objects of all captured events as
// "Kind/namespace/name" strings for order-independent assertions.
func (c *capturingRecorder) targets(t *testing.T) []string {
	t.Helper()
	out := make([]string, 0, len(c.events))
	for _, e := range c.events {
		switch o := e.object.(type) {
		case *rayv1.RayCluster:
			out = append(out, "RayCluster/"+o.Namespace+"/"+o.Name)
		case *rayv1.RayJob:
			out = append(out, "RayJob/"+o.Namespace+"/"+o.Name)
		default:
			t.Fatalf("unexpected event target type %T", e.object)
		}
	}
	return out
}

func forwarderScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, rayv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return scheme
}

func rayPodOnNode(name, namespace, cluster, node string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{utils.RayClusterLabelKey: cluster},
		},
		Spec: corev1.PodSpec{NodeName: node},
	}
}

func rayJobPodOnNode(name, namespace, cluster, rayJob, node string) *corev1.Pod {
	pod := rayPodOnNode(name, namespace, cluster, node)
	pod.Labels[utils.RayOriginatedFromCRDLabelKey] = utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD)
	pod.Labels[utils.RayOriginatedFromCRNameLabelKey] = rayJob
	return pod
}

// warningNodeEvent returns a legacy-style Warning Node event, as emitted by
// components like node-problem-detector.
func warningNodeEvent(name, node string) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(name + "-uid")},
		InvolvedObject: corev1.ObjectReference{Kind: "Node", Name: node},
		Source:         corev1.EventSource{Component: "node-problem-detector"},
		Reason:         "XIDError",
		Message:        "Caught XID error, XID=79",
		Type:           corev1.EventTypeWarning,
		Count:          1,
		LastTimestamp:  metav1.NewTime(time.Now()),
	}
}

// newEventForwarder builds a reconciler over a fake client that mirrors the
// production cache: a spec.nodeName field index over Pods. startedAt is set in
// the past so freshly stamped test events pass the replay guard.
func newEventForwarder(t *testing.T, recorder *capturingRecorder, options EventForwarderOptions, objs ...client.Object) *EventForwarderReconciler {
	t.Helper()
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(forwarderScheme(t)).
		WithObjects(objs...).
		WithIndex(&corev1.Pod{}, podNodeNameIndexField, func(o client.Object) []string {
			pod, ok := o.(*corev1.Pod)
			if !ok || pod.Spec.NodeName == "" {
				return nil
			}
			return []string{pod.Spec.NodeName}
		}).
		Build()
	return &EventForwarderReconciler{
		Client:    fakeClient,
		Recorder:  recorder,
		filter:    newEventFilter(options),
		startedAt: time.Now().Add(-time.Hour),
		forwarded: make(map[types.NamespacedName]forwardedRecord),
	}
}

func reconcileForwarderEvent(t *testing.T, r *EventForwarderReconciler, eventName string) {
	t.Helper()
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: eventName, Namespace: "default"},
	})
	require.NoError(t, err)
}
