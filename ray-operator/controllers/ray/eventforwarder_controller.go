package ray

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	// podNodeNameIndexField is the cache field-index key for a Pod's spec.nodeName.
	podNodeNameIndexField = "spec.nodeName"
	// nodeInvolvedObjectKind is the involvedObject.kind of the source Events we forward.
	nodeInvolvedObjectKind = "Node"
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

// forwardTarget is a Ray custom resource a Node Event is forwarded to.
type forwardTarget struct {
	kind utils.CRDType
	key  types.NamespacedName
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
//
// The Event watch itself must be scoped server-side (involvedObject.kind=Node)
// via the manager cache options; see managercache.EventForwarderCacheByObject.
type EventForwarderReconciler struct {
	client.Client
	Recorder events.EventRecorder

	filter eventFilter

	// startedAt is used to skip Events last observed before this controller
	// started, which the informer would otherwise replay on its initial list.
	// A recurrence of an old Event bumps its count and last-observed time, so
	// recurring faults still get forwarded.
	startedAt time.Time

	mu sync.Mutex
	// forwarded tracks, per source Event object, the occurrence that was last
	// forwarded. Keyed by object name so entries can be dropped when the API
	// server expires the Event (default TTL is 1h), keeping the map bounded by
	// the number of live Node Events.
	forwarded map[types.NamespacedName]forwardedRecord
}

// NewEventForwarderReconciler returns a new EventForwarderReconciler.
func NewEventForwarderReconciler(mgr manager.Manager, options EventForwarderOptions) *EventForwarderReconciler {
	return &EventForwarderReconciler{
		Client:    mgr.GetClient(),
		Recorder:  mgr.GetEventRecorder("kuberay-event-forwarder"),
		filter:    newEventFilter(options),
		startedAt: time.Now(),
		forwarded: make(map[types.NamespacedName]forwardedRecord),
	}
}

// SetupWithManager registers the Pod spec.nodeName field index and the Event watch.
func (r *EventForwarderReconciler) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
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

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;patch
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
	if eventLastObserved(src).Before(r.startedAt) {
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

	// Many Ray Pods may share one node; collapse to one Event per target
	// resource. Each Pod contributes its RayCluster, plus the root RayJob when
	// the Pod originated from one (ray.io/originated-from-* labels).
	targets := make(map[forwardTarget]struct{})
	for i := range pods.Items {
		pod := &pods.Items[i]
		if clusterName := pod.Labels[utils.RayClusterLabelKey]; clusterName != "" {
			targets[forwardTarget{
				kind: utils.RayClusterCRD,
				key:  types.NamespacedName{Namespace: pod.Namespace, Name: clusterName},
			}] = struct{}{}
		}
		if pod.Labels[utils.RayOriginatedFromCRDLabelKey] == utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD) {
			if crName := pod.Labels[utils.RayOriginatedFromCRNameLabelKey]; crName != "" {
				targets[forwardTarget{
					kind: utils.RayJobCRD,
					key:  types.NamespacedName{Namespace: pod.Namespace, Name: crName},
				}] = struct{}{}
			}
		}
	}
	if len(targets) == 0 {
		return ctrl.Result{}, nil
	}

	for target := range targets {
		obj, err := r.getTarget(ctx, target)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			// Retry the whole Event; markForwarded is not reached, so targets
			// already emitted to may see the Event again (at-least-once).
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(obj, src, src.Type, forwardedEventReason, "Forward",
			"Infrastructure failure detected on Node %q (reason: %s, source: %s): %s",
			nodeName, src.Reason, eventSource(src), src.Message)
		logger.V(1).Info("forwarded node event to Ray resource",
			"node", nodeName, "targetKind", target.kind, "target", target.key, "sourceReason", src.Reason)
	}

	r.markForwarded(request.NamespacedName, src)
	return ctrl.Result{}, nil
}

func (r *EventForwarderReconciler) getTarget(ctx context.Context, target forwardTarget) (client.Object, error) {
	var obj client.Object
	switch target.kind {
	case utils.RayJobCRD:
		obj = &rayv1.RayJob{}
	default:
		obj = &rayv1.RayCluster{}
	}
	if err := r.Get(ctx, target.key, obj); err != nil {
		return nil, err
	}
	return obj, nil
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
