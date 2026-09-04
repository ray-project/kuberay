package ray

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	// forwardedEventReason is the reason set on Events re-emitted onto Ray custom resources.
	forwardedEventReason = "NodeInfrastructureFailure"
	// maxEventNoteLength is the note length that events.k8s.io/v1 API server
	// validation enforces. An Event whose note exceeds it is rejected outright,
	// so a  verbose source message must be truncated rather than dropped.
	maxEventNoteLength = 1024
	// podNodeNameIndexField is the cache field-index key for a Pod's spec.nodeName.
	podNodeNameIndexField = "spec.nodeName"
	// nodeInvolvedObjectKind is the involvedObject.kind of the source Events we forward.
	nodeInvolvedObjectKind = "Node"
	// leadershipWaitInterval is how long to wait before requeueing an event if
	// Start has not yet recorded the leadership acquisition timestamp.
	leadershipWaitInterval = 100 * time.Millisecond
)

// EventForwarderOptions configures which Node Events are forwarded.
// An empty Sources or Reasons list means "allow all". An empty Types list
// defaults to forwarding only Warning events.
type EventForwarderOptions struct {
	// Sources is the allowed set of event emitters, matched against both the
	// legacy Source.Component and the new-style ReportingController fields
	// (e.g. "node-problem-detector", "nvidia-gpu-device-plugin").
	Sources []string
	// Reasons is the allowed set of event reasons (e.g. "XIDError", "KernelDeadlock").
	Reasons []string
	// Types is the allowed set of event types ("Warning", "Normal").
	Types []string
}

// Validate reports whether the options are usable. types is checked because an
// unrecognized value matches no Event and would otherwise silently disable forwarding altogether.
func (o EventForwarderOptions) Validate() error {
	for _, t := range o.Types {
		if t != corev1.EventTypeNormal && t != corev1.EventTypeWarning {
			return fmt.Errorf("invalid event type %q: must be one of %q, %q",
				t, corev1.EventTypeNormal, corev1.EventTypeWarning)
		}
	}
	return nil
}

// eventFilter is the compiled form of EventForwarderOptions.
type eventFilter struct {
	sources map[string]struct{}
	reasons map[string]struct{}
	types   map[string]struct{}
}

func newEventFilter(options EventForwarderOptions) eventFilter {
	if len(options.Types) == 0 {
		options.Types = []string{corev1.EventTypeWarning}
	}
	return eventFilter{
		sources: toSet(options.Sources),
		reasons: toSet(options.Reasons),
		types:   toSet(options.Types),
	}
}

// forwardedRecord remembers the last occurrence of a source Event that was
// forwarded, so that cache resyncs and requeues do not re-forward it while
// count bumps (recurrences of the same fault) do.
type forwardedRecord struct {
	uid   types.UID
	count int32
}

// EventForwarderReconciler watches Kubernetes Events involving Nodes and
// re-emits them onto the Ray custom resources (RayCluster, and the owning RayJob if any)
// whose Pods are scheduled on those Nodes, so they surface in the Ray Dashboard's Platform
// Events tab.
//
// The node->cluster join is served by a Pod field index on spec.nodeName: on
// each Node Event we list the Ray Pods on that node (filtered by the
// ray.io/cluster label), dedupe by target resource, and emit one Event per
// target with the involvedObject set to that resource.
type EventForwarderReconciler struct {
	client.Client
	Recorder events.EventRecorder

	filter eventFilter

	mu sync.Mutex
	// startedAt is used to skip Events last observed before this controller
	// started or acquired leadership, which the informer would otherwise
	// replay on its initial list or HA failover.
	// A recurrence of an old Event bumps its count and last-observed time, so
	// recurring faults still get forwarded.
	startedAt time.Time

	// forwarded tracks, per source Event object, the occurrence that was last
	// forwarded. Keyed by object name so entries can be dropped when the API
	// server expires the Event (default TTL is 1h), keeping the map bounded by
	// the number of live Node Events.
	forwarded map[types.NamespacedName]forwardedRecord
}

// NeedLeaderElection ensures Start is only called after this replica wins leader election.
func (r *EventForwarderReconciler) NeedLeaderElection() bool {
	return true
}

// Start records the exact timestamp when this replica acquired leadership, ensuring
// that cached Events predating leadership (informer replays from standby mode) are skipped.
func (r *EventForwarderReconciler) Start(ctx context.Context) error {
	r.mu.Lock()
	r.startedAt = time.Now().Truncate(time.Second)
	r.mu.Unlock()

	<-ctx.Done()
	return nil
}

func (r *EventForwarderReconciler) getStartedAt() (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startedAt, !r.startedAt.IsZero()
}

// NewEventForwarderReconciler returns a new EventForwarderReconciler or an error
// if the options are invalid.
func NewEventForwarderReconciler(mgr manager.Manager, options EventForwarderOptions) (*EventForwarderReconciler, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	return &EventForwarderReconciler{
		Client:    mgr.GetClient(),
		Recorder:  mgr.GetEventRecorder("kuberay-event-forwarder"),
		filter:    newEventFilter(options),
		forwarded: make(map[types.NamespacedName]forwardedRecord),
	}, nil
}

// SetupWithManager registers the Pod spec.nodeName field index and the Event watch.
func (r *EventForwarderReconciler) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
	if err := mgr.Add(r); err != nil {
		return err
	}

	// The cache cannot serve a MatchingFields List unless the field is indexed;
	// without this the List in Reconcile returns an error, not empty results.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{},
		podNodeNameIndexField, func(o client.Object) []string {
			pod, ok := o.(*corev1.Pod)
			if !ok || pod.Spec.NodeName == "" {
				return nil
			}
			return []string{pod.Spec.NodeName}
		}); err != nil {
		return err
	}

	// The predicate keeps non-matching Events out of the workqueue; the cache
	// field selector (involvedObject.kind=Node) already keeps non-Node Events
	// out of the informer entirely.
	forwardable := predicate.NewPredicateFuncs(func(o client.Object) bool {
		ev, ok := o.(*corev1.Event)
		return ok && ev.InvolvedObject.Kind == nodeInvolvedObjectKind && r.filter.matches(ev)
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named("event-forwarder").
		For(&corev1.Event{}, builder.WithPredicates(forwardable)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: reconcileConcurrency,
			LogConstructor: func(request *reconcile.Request) logr.Logger {
				logger := ctrl.Log.WithName("controllers").WithName("EventForwarder")
				if request != nil {
					logger = logger.WithValues("Event", request.NamespacedName)
				}
				return logger
			},
		}).
		Complete(r)
}

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=ray.io,resources=rayjobs,verbs=get;list;watch

// [WARNING]: There MUST be a newline after kubebuilder markers.

// Reconcile forwards a single Node Event onto every Ray custom resource with Pods on that Node.
func (r *EventForwarderReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	src := &corev1.Event{}
	if err := r.Get(ctx, request.NamespacedName, src); err != nil {
		if errors.IsNotFound(err) {
			// The API server expired or deleted the source Event; drop its
			// tracking entry so the forwarded map stays bounded.
			r.forget(request.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// The cache field selector and watch predicate already enforce these;
	// re-check here so behavior does not depend on how we are driven.
	if src.InvolvedObject.Kind != nodeInvolvedObjectKind || !r.filter.matches(src) {
		return ctrl.Result{}, nil
	}
	startedAt, leadershipRecorded := r.getStartedAt()
	// On HA failover, controller workers and Start() run concurrently upon leadership acquisition.
	// If a worker reconciles an event from the warm cache before Start() records the leadership timestamp,
	// requeue briefly so we do not evaluate event freshness against an uninitialized startedAt.
	if !leadershipRecorded {
		return ctrl.Result{RequeueAfter: leadershipWaitInterval}, nil
	}
	if eventLastObserved(src).Before(startedAt) {
		return ctrl.Result{}, nil
	}
	if !r.shouldForward(request.NamespacedName, src) {
		return ctrl.Result{}, nil
	}

	nodeName := src.InvolvedObject.Name

	// Served from the spec.nodeName field index. The manager cache only holds
	// Ray node Pods, and the label filter narrows to those with a cluster label.
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.MatchingFields{podNodeNameIndexField: nodeName},
		client.HasLabels{utils.RayClusterLabelKey},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Many Ray Pods may share one node; collapse to one Event per RayCluster.
	clusterKeys := make(map[types.NamespacedName]struct{})
	for i := range pods.Items {
		pod := &pods.Items[i]
		if clusterName := pod.Labels[utils.RayClusterLabelKey]; clusterName != "" {
			clusterKeys[types.NamespacedName{Namespace: pod.Namespace, Name: clusterName}] = struct{}{}
		}
	}

	if len(clusterKeys) == 0 {
		// No Ray workload on this node right now. The Event is deliberately not
		// marked forwarded, but this controller only watches Events, so a Pod
		// scheduled onto the node later will not retroactively receive it unless
		// the fault recurs.
		return ctrl.Result{}, nil
	}

	targets, err := r.resolveTargets(ctx, clusterKeys)
	if err != nil {
		// Retry the whole Event; markForwarded is not reached, so targets
		// already emitted to may see the Event again (at-least-once).
		return ctrl.Result{}, err
	}

	// The affected Node is attached as the related object so a reader of the
	// forwarded Event can get back to the source of the fault.
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, UID: src.InvolvedObject.UID}}
	note := truncatedEventNote(fmt.Sprintf(
		"Infrastructure failure detected on Node %q (reason: %s, source: %s): %s",
		nodeName, src.Reason, eventSource(src), src.Message))

	for _, target := range targets {
		r.Recorder.Eventf(target, node, src.Type, forwardedEventReason, "Forward", "%s", note)
		logger.V(1).Info("forwarded node event to Ray resource",
			"node", nodeName, "target", client.ObjectKeyFromObject(target), "sourceReason", src.Reason)
	}

	r.markForwarded(request.NamespacedName, src)
	return ctrl.Result{}, nil
}

// resolveTargets returns the Ray custom resources to forward to: every RayCluster
// in clusterKeys that still exists, plus the RayJob that created it, if any
func (r *EventForwarderReconciler) resolveTargets(ctx context.Context, clusterKeys map[types.NamespacedName]struct{}) ([]client.Object, error) {
	var targets []client.Object
	seenJobs := make(map[types.NamespacedName]struct{})

	for clusterKey := range clusterKeys {
		cluster := &rayv1.RayCluster{}
		if err := r.Get(ctx, clusterKey, cluster); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		targets = append(targets, cluster)

		jobKey, ok := owningRayJob(cluster)
		if !ok {
			continue
		}
		if _, dup := seenJobs[jobKey]; dup {
			continue
		}
		seenJobs[jobKey] = struct{}{}

		job := &rayv1.RayJob{}
		if err := r.Get(ctx, jobKey, job); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		targets = append(targets, job)
	}

	return targets, nil
}

func owningRayJob(cluster *rayv1.RayCluster) (types.NamespacedName, bool) {
	if utils.GetCRDType(cluster.Labels[utils.RayOriginatedFromCRDLabelKey]) != utils.RayJobCRD {
		return types.NamespacedName{}, false
	}

	name := cluster.Labels[utils.RayOriginatedFromCRNameLabelKey]
	if name == "" {
		return types.NamespacedName{}, false
	}

	return types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      name,
	}, true
}

func truncatedEventNote(note string) string {
	if len(note) <= maxEventNoteLength {
		return note
	}

	const ellipsis = "..."
	cut := maxEventNoteLength - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(note[cut]) {
		cut--
	}
	return note[:cut] + ellipsis
}

// shouldForward reports whether this occurrence of the source Event is new.
// Kubernetes aggregates recurring events by bumping count (or series.count) on
// the same object, so a higher count than last forwarded means the fault
// recurred. A different UID under the same name means a brand-new Event.
func (r *EventForwarderReconciler) shouldForward(key types.NamespacedName, e *corev1.Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	last, ok := r.forwarded[key]
	if !ok || last.uid != e.UID {
		return true
	}
	return occurrenceCount(e) > last.count
}

func (r *EventForwarderReconciler) markForwarded(key types.NamespacedName, e *corev1.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forwarded[key] = forwardedRecord{uid: e.UID, count: occurrenceCount(e)}
}

func (r *EventForwarderReconciler) forget(key types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.forwarded, key)
}

// occurrenceCount returns how many times the source Event has occurred,
// handling both legacy (count) and new-style (series.count) aggregation.
func occurrenceCount(e *corev1.Event) int32 {
	if e.Series != nil {
		return e.Series.Count
	}
	if e.Count > 0 {
		return e.Count
	}
	return 1
}

// eventLastObserved returns the most recent time the Event was observed,
// handling both legacy (lastTimestamp) and new-style (eventTime, series)
// Events.
func eventLastObserved(e *corev1.Event) time.Time {
	t := e.LastTimestamp.Time
	if e.Series != nil && e.Series.LastObservedTime.Time.After(t) {
		t = e.Series.LastObservedTime.Time
	}
	if e.EventTime.Time.After(t) {
		t = e.EventTime.Time
	}
	if t.IsZero() {
		t = e.CreationTimestamp.Time
	}
	return t
}

// eventSource returns the component that emitted the Event, handling both
// legacy (source.component) and new-style (reportingController) Events.
func eventSource(e *corev1.Event) string {
	if e.Source.Component != "" {
		return e.Source.Component
	}
	return e.ReportingController
}

func (f eventFilter) matches(e *corev1.Event) bool {
	if _, ok := f.types[e.Type]; !ok {
		return false
	}
	if len(f.reasons) > 0 {
		if _, ok := f.reasons[e.Reason]; !ok {
			return false
		}
	}
	if len(f.sources) > 0 {
		_, bySource := f.sources[e.Source.Component]
		_, byReportingController := f.sources[e.ReportingController]
		if !bySource && !byReportingController {
			return false
		}
	}
	return true
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}
