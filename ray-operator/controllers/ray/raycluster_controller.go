package ray

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/event"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	rbacv1 "k8s.io/api/rbac/v1"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr"
	_ "k8s.io/api/apps/v1beta1"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	DefaultRequeueDuration = 2 * time.Second
	ForcedClusterUpgrade   bool
	EnableBatchScheduler   bool

	// Definition of a index field for pod name
	podUIDIndexField = "metadata.uid"
)

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr manager.Manager) *RayClusterReconciler {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, podUIDIndexField, func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{string(pod.UID)}
	}); err != nil {
		panic(err)
	}

	return &RayClusterReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Log:               ctrl.Log.WithName("controllers").WithName("RayCluster"),
		Recorder:          mgr.GetEventRecorderFor("raycluster-controller"),
		BatchSchedulerMgr: batchscheduler.NewSchedulerManager(mgr.GetConfig()),
	}
}

var _ reconcile.Reconciler = &RayClusterReconciler{}

// RayClusterReconciler reconciles a RayCluster object
type RayClusterReconciler struct {
	client.Client
	Log               logr.Logger
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	BatchSchedulerMgr *batchscheduler.SchedulerManager
}

// Reconcile reads that state of the cluster for a RayCluster object and makes changes based on it
// and what is in the RayCluster.Spec
// Automatically generate RBAC rules to allow the Controller to read and write workloads
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups=extensions,resources=ingresses,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;delete;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;delete

// [WARNING]: There MUST be a newline after kubebuilder markers.
// Reconcile used to bridge the desired state with the current state
func (r *RayClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var err error

	// Try to fetch the Event instance
	event := &corev1.Event{}
	if err = r.Get(ctx, request.NamespacedName, event); err == nil {
		return r.eventReconcile(ctx, request, event)
	}

	// Try to fetch the RayCluster instance
	instance := &rayv1alpha1.RayCluster{}
	if err = r.Get(ctx, request.NamespacedName, instance); err == nil {
		return r.rayClusterReconcile(ctx, request, instance)
	}

	// No match found
	if errors.IsNotFound(err) {
		r.Log.Info("Read request instance not found error!", "name", request.NamespacedName)
	} else {
		r.Log.Error(err, "Read request instance error!")
	}
	// Error reading the object - requeue the request.
	return ctrl.Result{}, client.IgnoreNotFound(err)
}

func (r *RayClusterReconciler) eventReconcile(ctx context.Context, request ctrl.Request, event *corev1.Event) (ctrl.Result, error) {
	var unhealthyPod *corev1.Pod
	pods := corev1.PodList{}

	// we only care about pod events
	if event.InvolvedObject.Kind != "Pod" || event.Type != "Warning" || event.Reason != "Unhealthy" ||
		!strings.Contains(event.Message, "Readiness probe failed") {
		// This is not supposed to happen since we already filter events in the watch
		msg := fmt.Sprintf("unexpected event, we should have already filtered these conditions: %v", event)
		r.Log.Error(fmt.Errorf(msg), msg, "event", event)
		return ctrl.Result{}, nil
	}

	_ = r.Log.WithValues("event", request.NamespacedName)

	options := []client.ListOption{
		client.MatchingFields(map[string]string{podUIDIndexField: string(event.InvolvedObject.UID)}),
		client.InNamespace(event.InvolvedObject.Namespace),
		client.MatchingLabels(map[string]string{common.RayNodeLabelKey: "yes"}),
	}

	if err := r.List(ctx, &pods, options...); err != nil {
		return ctrl.Result{}, err
	}

	if len(pods.Items) == 0 {
		r.Log.Info("no ray node pod found for event", "event", event)
		return ctrl.Result{}, nil
	} else if len(pods.Items) > 1 {
		// This happens when we use fake client
		r.Log.Info("are you running in test mode?")
		for _, pod := range pods.Items {
			if pod.Name == event.InvolvedObject.Name {
				unhealthyPod = &pod
				break
			}
		}
	} else {
		r.Log.Info("found unhealthy ray node", "pod name", event.InvolvedObject.Name)
		unhealthyPod = &pods.Items[0]
	}

	if unhealthyPod.Annotations == nil {
		r.Log.Info("The unhealthy ray node not found", "pod name", event.InvolvedObject.Name)
		return ctrl.Result{}, nil
	}

	if enabledString, ok := unhealthyPod.Annotations[common.RayFTEnabledAnnotationKey]; ok {
		if strings.ToLower(enabledString) != "true" {
			r.Log.Info("FT not enabled skipping event reconcile for pod.", "pod name", unhealthyPod.Name)
			return ctrl.Result{}, nil
		}
	} else {
		r.Log.Info("HAEnabled annotation not found", "pod name", unhealthyPod.Name)
		return ctrl.Result{}, nil
	}

	if !utils.IsRunningAndReady(unhealthyPod) {
		if v, ok := unhealthyPod.Annotations[common.RayNodeHealthStateAnnotationKey]; !ok || v != common.PodUnhealthy {
			updatedPod := unhealthyPod.DeepCopy()
			updatedPod.Annotations[common.RayNodeHealthStateAnnotationKey] = common.PodUnhealthy
			r.Log.Info("mark pod unhealthy and need for a rebuild", "pod", unhealthyPod)
			if err := r.Update(ctx, updatedPod); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *RayClusterReconciler) rayClusterReconcile(ctx context.Context, request ctrl.Request, instance *rayv1alpha1.RayCluster) (ctrl.Result, error) {
	// Please do NOT modify `originalRayClusterInstance` in the following code.
	originalRayClusterInstance := instance.DeepCopy()

	_ = r.Log.WithValues("raycluster", request.NamespacedName)
	r.Log.Info("reconciling RayCluster", "cluster name", request.Name)

	if instance.DeletionTimestamp != nil && !instance.DeletionTimestamp.IsZero() {
		r.Log.Info("RayCluster is being deleted, just ignore", "cluster name", request.Name)
		return ctrl.Result{}, nil
	}

	if err := r.reconcileAutoscalerServiceAccount(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	if err := r.reconcileAutoscalerRole(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileAutoscalerRoleBinding(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileIngress(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileHeadService(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcilePods(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		if updateErr := r.updateClusterReason(ctx, instance, err.Error()); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update reason error", "cluster name", request.Name)
		}
		r.Recorder.Event(instance, corev1.EventTypeWarning, string(rayv1alpha1.PodReconciliationError), err.Error())
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// Calculate the new status for the RayCluster. Note that the function will deep copy `instance` instead of mutating it.
	newInstance, err := r.calculateStatus(ctx, instance)
	if err != nil {
		r.Log.Info("Got error when calculating new status", "cluster name", request.Name, "error", err)
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// Check if need to update the status.
	if r.inconsistentRayClusterStatus(originalRayClusterInstance.Status, newInstance.Status) {
		r.Log.Info("rayClusterReconcile", "Update CR status", request.Name, "status", newInstance.Status)
		if err := r.Status().Update(ctx, newInstance); err != nil {
			r.Log.Info("Got error when updating status", "cluster name", request.Name, "error", err, "RayCluster", newInstance)
			return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
		}
	}

	// Unconditionally requeue after the number of seconds specified in the
	// environment variable RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV. If the
	// environment variable is not set, requeue after the default value.
	requeueAfterSeconds, err := strconv.Atoi(os.Getenv(common.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV))
	if err != nil {
		r.Log.Info(fmt.Sprintf("Environment variable %s is not set, using default value of %d seconds", common.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV, common.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS), "cluster name", request.Name)
		requeueAfterSeconds = common.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS
	}
	r.Log.Info("Unconditional requeue after", "cluster name", request.Name, "seconds", requeueAfterSeconds)
	return ctrl.Result{RequeueAfter: time.Duration(requeueAfterSeconds) * time.Second}, nil
}

// Checks whether the old and new RayClusterStatus are inconsistent by comparing different fields. If the only
// differences between the old and new status are the `LastUpdateTime` and `ObservedGeneration` fields, the
// status update will not be triggered.
//
// TODO (kevin85421): The field `ObservedGeneration` is not being well-maintained at the moment. In the future,
// this field should be used to determine whether to update this CR or not.
func (r *RayClusterReconciler) inconsistentRayClusterStatus(oldStatus rayv1alpha1.RayClusterStatus, newStatus rayv1alpha1.RayClusterStatus) bool {
	if oldStatus.State != newStatus.State || oldStatus.Reason != newStatus.Reason {
		r.Log.Info("inconsistentRayClusterStatus", "detect inconsistency", fmt.Sprintf(
			"old State: %s, new State: %s, old Reason: %s, new Reason: %s",
			oldStatus.State, newStatus.State, oldStatus.Reason, newStatus.Reason))
		return true
	}
	if oldStatus.AvailableWorkerReplicas != newStatus.AvailableWorkerReplicas || oldStatus.DesiredWorkerReplicas != newStatus.DesiredWorkerReplicas ||
		oldStatus.MinWorkerReplicas != newStatus.MinWorkerReplicas || oldStatus.MaxWorkerReplicas != newStatus.MaxWorkerReplicas {
		r.Log.Info("inconsistentRayClusterStatus", "detect inconsistency", fmt.Sprintf(
			"old AvailableWorkerReplicas: %d, new AvailableWorkerReplicas: %d, old DesiredWorkerReplicas: %d, new DesiredWorkerReplicas: %d, "+
				"old MinWorkerReplicas: %d, new MinWorkerReplicas: %d, old MaxWorkerReplicas: %d, new MaxWorkerReplicas: %d",
			oldStatus.AvailableWorkerReplicas, newStatus.AvailableWorkerReplicas, oldStatus.DesiredWorkerReplicas, newStatus.DesiredWorkerReplicas,
			oldStatus.MinWorkerReplicas, newStatus.MinWorkerReplicas, oldStatus.MaxWorkerReplicas, newStatus.MaxWorkerReplicas))
		return true
	}
	if !reflect.DeepEqual(oldStatus.Endpoints, newStatus.Endpoints) || !reflect.DeepEqual(oldStatus.Head, newStatus.Head) {
		r.Log.Info("inconsistentRayClusterStatus", "detect inconsistency", fmt.Sprintf(
			"old Endpoints: %v, new Endpoints: %v, old Head: %v, new Head: %v",
			oldStatus.Endpoints, newStatus.Endpoints, oldStatus.Head, newStatus.Head))
		return true
	}
	return false
}

func (r *RayClusterReconciler) reconcileIngress(ctx context.Context, instance *rayv1alpha1.RayCluster) error {
	if instance.Spec.HeadGroupSpec.EnableIngress == nil || !*instance.Spec.HeadGroupSpec.EnableIngress {
		return nil
	}

	headIngresses := networkingv1.IngressList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: instance.Name}
	if err := r.List(ctx, &headIngresses, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}

	if headIngresses.Items != nil && len(headIngresses.Items) == 1 {
		r.Log.Info("reconcileIngresses", "head service ingress found", headIngresses.Items[0].Name)
		return nil
	}

	if headIngresses.Items == nil || len(headIngresses.Items) == 0 {
		ingress, err := common.BuildIngressForHeadService(*instance)
		if err != nil {
			return err
		}

		if err := ctrl.SetControllerReference(instance, ingress, r.Scheme); err != nil {
			return err
		}

		err = r.createHeadIngress(ctx, ingress, instance)
		if err != nil {
			return err
		}
	}

	return nil
}

// Return nil only when the head service successfully created or already exists.
func (r *RayClusterReconciler) reconcileHeadService(ctx context.Context, instance *rayv1alpha1.RayCluster) error {
	services := corev1.ServiceList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode)}

	if err := r.List(ctx, &services, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}

	// Check if there's existing head service in the cluster.
	if len(services.Items) != 0 {
		if len(services.Items) == 1 {
			r.Log.Info("reconcileHeadService", "1 head service found", services.Items[0].Name)
			return nil
		}
		// This should never happen. This protects against the case that users manually create service with the same label.
		if len(services.Items) > 1 {
			r.Log.Info("reconcileHeadService", "Duplicate head service found", services.Items)
			return fmt.Errorf("%d head service found %v", len(services.Items), services.Items)
		}
	} else {
		// Create head service if there's no existing one in the cluster.
		labels := make(map[string]string)
		if val, ok := instance.Spec.HeadGroupSpec.Template.ObjectMeta.Labels[common.KubernetesApplicationNameLabelKey]; ok {
			labels[common.KubernetesApplicationNameLabelKey] = val
		}
		annotations := make(map[string]string)
		// TODO (kevin85421): KubeRay has already exposed the entire head service (#1040) to users.
		// We may consider deprecating this field when we bump the CRD version.
		for k, v := range instance.Spec.HeadServiceAnnotations {
			annotations[k] = v
		}
		headSvc, err := common.BuildServiceForHeadPod(*instance, labels, annotations)
		// TODO (kevin85421): Provide a detailed and actionable error message. For example, which port is missing?
		if len(headSvc.Spec.Ports) == 0 {
			r.Log.Info("Ray head service does not have any ports set up. Service specification: %v", headSvc.Spec)
			return fmt.Errorf("Ray head service does not have any ports set up. Service specification: %v", headSvc.Spec)
		}

		if err != nil {
			return err
		}

		if err := r.createService(ctx, headSvc, instance); err != nil {
			return err
		}
	}

	return nil
}

func (r *RayClusterReconciler) reconcilePods(ctx context.Context, instance *rayv1alpha1.RayCluster) error {
	// check if all the pods exist
	headPods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode)}
	if err := r.List(ctx, &headPods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}
	if EnableBatchScheduler {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(instance); err == nil {
			if err := scheduler.DoBatchSchedulingOnSubmission(instance); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Reconcile head Pod
	if len(headPods.Items) == 1 {
		headPod := headPods.Items[0]
		r.Log.Info("reconcilePods", "head pod found", headPod.Name)
		if headPod.Status.Phase == corev1.PodRunning || headPod.Status.Phase == corev1.PodPending {
			r.Log.Info("reconcilePods", "head pod is up and running... checking workers", headPod.Name)
		} else if headPod.Status.Phase == corev1.PodFailed && strings.Contains(headPod.Status.Reason, "Evicted") {
			// Handle evicted pod
			r.Log.Info("reconcilePods", "head pod has been evicted and controller needs to replace the pod", headPod.Name)
			if err := r.Delete(ctx, &headPod); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("head pod %s is not running nor pending", headPod.Name)
		}
	}
	if len(headPods.Items) == 0 || headPods.Items == nil {
		// create head pod
		r.Log.Info("reconcilePods", "creating head pod for cluster", instance.Name)
		common.CreatedClustersCounterInc(instance.Namespace)
		if err := r.createHeadPod(ctx, *instance); err != nil {
			common.FailedClustersCounterInc(instance.Namespace)
			return err
		}
		common.SuccessfulClustersCounterInc(instance.Namespace)
	} else if len(headPods.Items) > 1 {
		r.Log.Info("reconcilePods", "more than 1 head pod found for cluster", instance.Name)
		itemLength := len(headPods.Items)
		for index := 0; index < itemLength; index++ {
			if headPods.Items[index].Status.Phase == corev1.PodRunning || headPods.Items[index].Status.Phase == corev1.PodPending {
				// Remove the healthy pod  at index i from the list of pods to delete
				headPods.Items[index] = headPods.Items[len(headPods.Items)-1] // replace last element with the healthy head.
				headPods.Items = headPods.Items[:len(headPods.Items)-1]       // Truncate slice.
				itemLength--
			}
		}
		// delete all the extra head pod pods
		for _, extraHeadPodToDelete := range headPods.Items {
			if err := r.Delete(ctx, &extraHeadPodToDelete); err != nil {
				return err
			}
		}
	} else {
		// we have exactly one head pod running
		if headPods.Items[0].Annotations != nil {
			if v, ok := headPods.Items[0].Annotations[common.RayNodeHealthStateAnnotationKey]; ok && v == common.PodUnhealthy {
				if err := r.Delete(ctx, &headPods.Items[0]); err != nil {
					return err
				}
				r.Log.Info(fmt.Sprintf("need to delete unhealthy head pod %s", headPods.Items[0].Name))
				// we are deleting the head pod now, let's reconcile again later
				return nil
			}
		}
	}

	if ForcedClusterUpgrade {
		if len(headPods.Items) == 1 {
			// head node amount is exactly 1, but we need to check if it has been changed
			res := utils.PodNotMatchingTemplate(headPods.Items[0], instance.Spec.HeadGroupSpec.Template)
			if res {
				r.Log.Info(fmt.Sprintf("need to delete old head pod %s", headPods.Items[0].Name))
				if err := r.Delete(ctx, &headPods.Items[0]); err != nil {
					return err
				}
				return nil
			}
		}

		// check if WorkerGroupSpecs has been changed and we need to kill worker pods
		for _, worker := range instance.Spec.WorkerGroupSpecs {
			workerPods := corev1.PodList{}
			filterLabels = client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeGroupLabelKey: worker.GroupName}
			if err := r.List(ctx, &workerPods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
				return err
			}
			updatedWorkerPods := false
			for _, item := range workerPods.Items {
				if utils.PodNotMatchingTemplate(item, worker.Template) {
					r.Log.Info(fmt.Sprintf("need to delete old worker pod %s", item.Name))
					if err := r.Delete(ctx, &item); err != nil {
						r.Log.Info(fmt.Sprintf("error deleting worker pod %s", item.Name))
						return err
					}
					updatedWorkerPods = true
				}
			}
			if updatedWorkerPods {
				return nil
			}
		}
	}

	// Reconcile worker pods now
	for _, worker := range instance.Spec.WorkerGroupSpecs {
		// workerReplicas will store the target number of pods for this worker group.
		var workerReplicas int32
		// Always honor MaxReplicas if it is set:
		// If MaxReplicas is set and Replicas > MaxReplicas, use MaxReplicas as the
		// effective target replica count and log the discrepancy.
		// See https://github.com/ray-project/kuberay/issues/560.
		if worker.MaxReplicas != nil && *worker.MaxReplicas < *worker.Replicas {
			workerReplicas = *worker.MaxReplicas
			r.Log.Info(
				fmt.Sprintf(
					"Replicas for worker group %s (%d) is greater than maxReplicas (%d). Using maxReplicas (%d) as the target replica count.",
					worker.GroupName, *worker.Replicas, *worker.MaxReplicas, *worker.MaxReplicas,
				),
			)
		} else {
			workerReplicas = *worker.Replicas
		}
		workerPods := corev1.PodList{}
		filterLabels = client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeGroupLabelKey: worker.GroupName}
		if err := r.List(ctx, &workerPods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
			return err
		}

		// delete the worker pod if it is marked unhealthy
		for _, workerPod := range workerPods.Items {
			if workerPod.Annotations == nil {
				continue
			}
			if v, ok := workerPod.Annotations[common.RayNodeHealthStateAnnotationKey]; ok && v == common.PodUnhealthy {
				r.Log.Info(fmt.Sprintf("deleting unhealthy worker pod %s", workerPod.Name))
				if err := r.Delete(ctx, &workerPod); err != nil {
					return err
				}
				// we are deleting one worker pod now, let's reconcile again later
				return nil
			}
		}

		// Always remove the specified WorkersToDelete - regardless of the value of Replicas.
		// Essentially WorkersToDelete has to be deleted to meet the expectations of the Autoscaler.
		deletedWorkers := make(map[string]struct{})
		deleted := struct{}{}
		r.Log.Info("reconcilePods", "removing the pods in the scaleStrategy of", worker.GroupName)
		for _, podsToDelete := range worker.ScaleStrategy.WorkersToDelete {
			pod := corev1.Pod{}
			pod.Name = podsToDelete
			pod.Namespace = utils.GetNamespace(instance.ObjectMeta)
			r.Log.Info("Deleting pod", "namespace", pod.Namespace, "name", pod.Name)
			if err := r.Delete(ctx, &pod); err != nil {
				if !errors.IsNotFound(err) {
					r.Log.Info("reconcilePods", "Fail to delete Pod", pod.Name, "error", err)
					return err
				}
				r.Log.Info("reconcilePods", "The worker Pod has already been deleted", pod.Name)
			} else {
				deletedWorkers[pod.Name] = deleted
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted", "Deleted pod %s", pod.Name)
			}
		}
		worker.ScaleStrategy.WorkersToDelete = []string{}

		runningPods := corev1.PodList{}
		for _, pod := range workerPods.Items {
			// TODO (kevin85421): We also need to have a clear story of all the Pod status phases, especially for PodFailed.
			if _, ok := deletedWorkers[pod.Name]; !ok && isPodRunningOrPendingAndNotDeleting(pod) {
				runningPods.Items = append(runningPods.Items, pod)
			}
		}
		diff := workerReplicas - int32(len(runningPods.Items))
		r.Log.Info("reconcilePods", "workerReplicas", workerReplicas, "runningPods", len(runningPods.Items), "diff", diff)

		if diff > 0 {
			// pods need to be added
			r.Log.Info("reconcilePods", "Number workers to add", diff, "Worker group", worker.GroupName)
			// create all workers of this group
			var i int32
			for i = 0; i < diff; i++ {
				r.Log.Info("reconcilePods", "creating worker for group", worker.GroupName, fmt.Sprintf("index %d", i), fmt.Sprintf("in total %d", diff))
				if err := r.createWorkerPod(ctx, *instance, *worker.DeepCopy()); err != nil {
					return err
				}
			}
		} else if diff == 0 {
			r.Log.Info("reconcilePods", "all workers already exist for group", worker.GroupName)
			continue
		} else {
			// diff < 0 indicates the need to delete some Pods to match the desired number of replicas. However,
			// randomly deleting Pods is certainly not ideal. So, if autoscaling is enabled for the cluster, we
			// will disable random Pod deletion, making Autoscaler the sole decision-maker for Pod deletions.
			enableInTreeAutoscaling := (instance.Spec.EnableInTreeAutoscaling != nil) && (*instance.Spec.EnableInTreeAutoscaling)

			// TODO (kevin85421): `enableRandomPodDelete` is a feature flag for KubeRay v0.6.0. If users want to use
			// the old behavior, they can set the environment variable `ENABLE_RANDOM_POD_DELETE` to `true`. When the
			// default behavior is stable enough, we can remove this feature flag.
			enableRandomPodDelete := false
			if enableInTreeAutoscaling {
				if s := os.Getenv(common.ENABLE_RANDOM_POD_DELETE); strings.ToLower(s) == "true" {
					enableRandomPodDelete = true
				}
			}
			// Case 1: If Autoscaler is disabled, we will always enable random Pod deletion no matter the value of the feature flag.
			// Case 2: If Autoscaler is enabled, we will respect the value of the feature flag. If the feature flag environment variable
			// is not set, we will disable random Pod deletion by default.
			if !enableInTreeAutoscaling || enableRandomPodDelete {
				// diff < 0 means that we need to delete some Pods to meet the desired number of replicas.
				randomlyRemovedWorkers := -diff
				r.Log.Info("reconcilePods", "Number workers to delete randomly", randomlyRemovedWorkers, "Worker group", worker.GroupName)
				for i := 0; i < int(randomlyRemovedWorkers); i++ {
					randomPodToDelete := runningPods.Items[i]
					r.Log.Info("Randomly deleting Pod", "progress", fmt.Sprintf("%d / %d", i+1, randomlyRemovedWorkers), "with name", randomPodToDelete.Name)
					if err := r.Delete(ctx, &randomPodToDelete); err != nil {
						if !errors.IsNotFound(err) {
							return err
						}
						r.Log.Info("reconcilePods", "The worker Pod has already been deleted", randomPodToDelete.Name)
					}
					r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted", "Deleted Pod %s", randomPodToDelete.Name)
				}
			} else {
				r.Log.Info(fmt.Sprintf("Random Pod deletion is disabled for cluster %s. The only decision-maker for Pod deletions is Autoscaler.", instance.Name))
			}
		}
	}
	return nil
}

func isPodRunningOrPendingAndNotDeleting(pod corev1.Pod) bool {
	return (pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending) && pod.ObjectMeta.DeletionTimestamp == nil
}

func (r *RayClusterReconciler) createHeadIngress(ctx context.Context, ingress *networkingv1.Ingress, instance *rayv1alpha1.RayCluster) error {
	// making sure the name is valid
	ingress.Name = utils.CheckName(ingress.Name)
	if err := controllerutil.SetControllerReference(instance, ingress, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, ingress); err != nil {
		if errors.IsAlreadyExists(err) {
			r.Log.Info("Ingress already exists, no need to create")
			return nil
		}
		r.Log.Error(err, "Ingress create error!", "Ingress.Error", err)
		return err
	}
	r.Log.Info("Ingress created successfully", "ingress name", ingress.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created ingress %s", ingress.Name)
	return nil
}

func (r *RayClusterReconciler) createService(ctx context.Context, raySvc *corev1.Service, instance *rayv1alpha1.RayCluster) error {
	// making sure the name is valid
	raySvc.Name = utils.CheckName(raySvc.Name)
	// Set controller reference
	if err := controllerutil.SetControllerReference(instance, raySvc, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, raySvc); err != nil {
		if errors.IsAlreadyExists(err) {
			r.Log.Info("Pod service already exist, no need to create")
			return nil
		}
		r.Log.Error(err, "Pod Service create error!", "Pod.Service.Error", err)
		return err
	}
	r.Log.Info("Pod Service created successfully", "service name", raySvc.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created service %s", raySvc.Name)
	return nil
}

func (r *RayClusterReconciler) createHeadPod(ctx context.Context, instance rayv1alpha1.RayCluster) error {
	// build the pod then create it
	pod := r.buildHeadPod(instance)
	podIdentifier := types.NamespacedName{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}
	if EnableBatchScheduler {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(&instance); err == nil {
			scheduler.AddMetadataToPod(&instance, &pod)
		} else {
			return err
		}
	}

	r.Log.Info("createHeadPod", "head pod with name", pod.GenerateName)
	if err := r.Create(ctx, &pod); err != nil {
		if errors.IsAlreadyExists(err) {
			fetchedPod := corev1.Pod{}
			// the pod might be in terminating state, we need to check
			if errPod := r.Get(ctx, podIdentifier, &fetchedPod); errPod == nil {
				if fetchedPod.DeletionTimestamp != nil {
					r.Log.Error(errPod, "create pod error!", "pod is in a terminating state, we will wait until it is cleaned up", podIdentifier)
					return err
				}
			}
			r.Log.Info("Creating pod", "Pod already exists", pod.Name)
		} else {
			return err
		}
	}
	r.Recorder.Eventf(&instance, corev1.EventTypeNormal, "Created", "Created head pod %s", pod.Name)
	return nil
}

func (r *RayClusterReconciler) createWorkerPod(ctx context.Context, instance rayv1alpha1.RayCluster, worker rayv1alpha1.WorkerGroupSpec) error {
	// build the pod then create it
	pod := r.buildWorkerPod(instance, worker)
	podIdentifier := types.NamespacedName{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}
	if EnableBatchScheduler {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(&instance); err == nil {
			scheduler.AddMetadataToPod(&instance, &pod)
		} else {
			return err
		}
	}

	replica := pod
	if err := r.Create(ctx, &replica); err != nil {
		if errors.IsAlreadyExists(err) {
			fetchedPod := corev1.Pod{}
			// the pod might be in terminating state, we need to check
			if errPod := r.Get(ctx, podIdentifier, &fetchedPod); errPod == nil {
				if fetchedPod.DeletionTimestamp != nil {
					r.Log.Error(errPod, "create pod error!", "pod is in a terminating state, we will wait until it is cleaned up", podIdentifier)
					return err
				}
			}
			r.Log.Info("Creating pod", "Pod already exists", pod.Name)
		} else {
			r.Log.Error(fmt.Errorf("createWorkerPod error"), "error creating pod", "pod", pod, "err = ", err)
			return err
		}
	}
	r.Log.Info("Created pod", "Pod ", pod.GenerateName)
	r.Recorder.Eventf(&instance, corev1.EventTypeNormal, "Created", "Created worker pod %s", pod.Name)
	return nil
}

// Build head instance pod(s).
func (r *RayClusterReconciler) buildHeadPod(instance rayv1alpha1.RayCluster) corev1.Pod {
	podName := strings.ToLower(instance.Name + common.DashSymbol + string(rayv1alpha1.HeadNode) + common.DashSymbol)
	podName = utils.CheckName(podName)                                            // making sure the name is valid
	fqdnRayIP := utils.GenerateFQDNServiceName(instance.Name, instance.Namespace) // Fully Qualified Domain Name
	// The Ray head port used by workers to connect to the cluster (GCS server port for Ray >= 1.11.0, Redis port for older Ray.)
	headPort := common.GetHeadPort(instance.Spec.HeadGroupSpec.RayStartParams)
	autoscalingEnabled := instance.Spec.EnableInTreeAutoscaling
	podConf := common.DefaultHeadPodTemplate(instance, instance.Spec.HeadGroupSpec, podName, headPort)
	r.Log.Info("head pod labels", "labels", podConf.Labels)
	creatorName := getCreator(instance)
	pod := common.BuildPod(podConf, rayv1alpha1.HeadNode, instance.Spec.HeadGroupSpec.RayStartParams, headPort, autoscalingEnabled, creatorName, fqdnRayIP)
	// Set raycluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(&instance, &pod, r.Scheme); err != nil {
		r.Log.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

func getCreator(instance rayv1alpha1.RayCluster) string {
	if instance.Labels == nil {
		return ""
	}
	creatorName, exist := instance.Labels[common.KubernetesCreatedByLabelKey]

	if !exist {
		return ""
	}

	return creatorName
}

// Build worker instance pods.
func (r *RayClusterReconciler) buildWorkerPod(instance rayv1alpha1.RayCluster, worker rayv1alpha1.WorkerGroupSpec) corev1.Pod {
	podName := strings.ToLower(instance.Name + common.DashSymbol + string(rayv1alpha1.WorkerNode) + common.DashSymbol + worker.GroupName + common.DashSymbol)
	podName = utils.CheckName(podName)                                            // making sure the name is valid
	fqdnRayIP := utils.GenerateFQDNServiceName(instance.Name, instance.Namespace) // Fully Qualified Domain Name
	// The Ray head port used by workers to connect to the cluster (GCS server port for Ray >= 1.11.0, Redis port for older Ray.)
	headPort := common.GetHeadPort(instance.Spec.HeadGroupSpec.RayStartParams)
	autoscalingEnabled := instance.Spec.EnableInTreeAutoscaling
	podTemplateSpec := common.DefaultWorkerPodTemplate(instance, worker, podName, fqdnRayIP, headPort)
	creatorName := getCreator(instance)
	pod := common.BuildPod(podTemplateSpec, rayv1alpha1.WorkerNode, worker.RayStartParams, headPort, autoscalingEnabled, creatorName, fqdnRayIP)
	// Set raycluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(&instance, &pod, r.Scheme); err != nil {
		r.Log.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

// SetupWithManager builds the reconciler.
func (r *RayClusterReconciler) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
	b := ctrl.NewControllerManagedBy(mgr).
		Named("raycluster-controller").
		For(&rayv1alpha1.RayCluster{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		Watches(&source.Kind{Type: &corev1.Event{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					if eventObj, ok := e.Object.(*corev1.Event); ok {
						if eventObj.InvolvedObject.Kind != "Pod" || eventObj.Type != "Warning" ||
							eventObj.Reason != "Unhealthy" || !strings.Contains(eventObj.Message, "Readiness probe failed") {
							// only care about pod unhealthy events
							return false
						}
						return true
					}
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			}),
		).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{})

	if EnableBatchScheduler {
		b = batchscheduler.ConfigureReconciler(b)
	}

	return b.
		WithOptions(controller.Options{MaxConcurrentReconciles: reconcileConcurrency}).
		Complete(r)
}

func (r *RayClusterReconciler) calculateStatus(ctx context.Context, instance *rayv1alpha1.RayCluster) (*rayv1alpha1.RayCluster, error) {
	// Deep copy the instance, so we don't mutate the original object.
	newInstance := instance.DeepCopy()

	// TODO (kevin85421): ObservedGeneration should be used to determine whether to update this CR or not.
	newInstance.Status.ObservedGeneration = newInstance.ObjectMeta.Generation

	runtimePods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: newInstance.Name}
	if err := r.List(ctx, &runtimePods, client.InNamespace(newInstance.Namespace), filterLabels); err != nil {
		return nil, err
	}

	newInstance.Status.AvailableWorkerReplicas = utils.CalculateAvailableReplicas(runtimePods)
	newInstance.Status.DesiredWorkerReplicas = utils.CalculateDesiredReplicas(newInstance)
	newInstance.Status.MinWorkerReplicas = utils.CalculateMinReplicas(newInstance)
	newInstance.Status.MaxWorkerReplicas = utils.CalculateMaxReplicas(newInstance)

	// validation for the RayStartParam for the state.
	isValid, err := common.ValidateHeadRayStartParams(newInstance.Spec.HeadGroupSpec)
	if err != nil {
		r.Recorder.Event(newInstance, corev1.EventTypeWarning, string(rayv1alpha1.RayConfigError), err.Error())
	}
	// only in invalid status that we update the status to unhealthy.
	if !isValid {
		newInstance.Status.State = rayv1alpha1.Unhealthy
	} else {
		if utils.CheckAllPodsRunning(runtimePods) {
			newInstance.Status.State = rayv1alpha1.Ready
		}
	}

	if err := r.updateEndpoints(ctx, newInstance); err != nil {
		return nil, err
	}

	if err := r.updateHeadInfo(ctx, newInstance); err != nil {
		return nil, err
	}

	timeNow := metav1.Now()
	newInstance.Status.LastUpdateTime = &timeNow

	return newInstance, nil
}

// Best effort to obtain the ip of the head node.
func (r *RayClusterReconciler) getHeadPodIP(ctx context.Context, instance *rayv1alpha1.RayCluster) (string, error) {
	runtimePods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode)}
	if err := r.List(ctx, &runtimePods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		r.Log.Error(err, "Failed to list pods while getting head pod ip.")
		return "", err
	}
	if len(runtimePods.Items) != 1 {
		r.Log.Info(fmt.Sprintf("Found %d head pods. cluster name %s, filter labels %v", len(runtimePods.Items), instance.Name, filterLabels))
		return "", nil
	}
	return runtimePods.Items[0].Status.PodIP, nil
}

func (r *RayClusterReconciler) getHeadServiceIP(ctx context.Context, instance *rayv1alpha1.RayCluster) (string, error) {
	runtimeServices := corev1.ServiceList{}
	filterLabels := client.MatchingLabels(common.HeadServiceLabels(*instance))
	if err := r.List(ctx, &runtimeServices, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return "", err
	}
	if len(runtimeServices.Items) < 1 {
		return "", fmt.Errorf("unable to find head service. cluster name %s, filter labels %v", instance.Name, filterLabels)
	} else if len(runtimeServices.Items) > 1 {
		return "", fmt.Errorf("found multiple head services. cluster name %s, filter labels %v", instance.Name, filterLabels)
	} else if runtimeServices.Items[0].Spec.ClusterIP == "" {
		return "", fmt.Errorf("head service IP is empty. cluster name %s, filter labels %v", instance.Name, filterLabels)
	}

	return runtimeServices.Items[0].Spec.ClusterIP, nil
}

func (r *RayClusterReconciler) updateEndpoints(ctx context.Context, instance *rayv1alpha1.RayCluster) error {
	// TODO: (@scarlet25151) There may be several K8s Services for a RayCluster.
	// We assume we can find the right one by filtering Services with appropriate label selectors
	// and picking the first one. We may need to select by name in the future if the Service naming is stable.
	rayHeadSvc := corev1.ServiceList{}
	filterLabels := client.MatchingLabels{
		common.RayClusterLabelKey:  instance.Name,
		common.RayNodeTypeLabelKey: "head",
	}
	if err := r.List(ctx, &rayHeadSvc, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}

	if len(rayHeadSvc.Items) != 0 {
		svc := rayHeadSvc.Items[0]
		if instance.Status.Endpoints == nil {
			instance.Status.Endpoints = map[string]string{}
		}
		for _, port := range svc.Spec.Ports {
			if len(port.Name) == 0 {
				r.Log.Info("updateStatus", "service port's name is empty. Not adding it to RayCluster status.endpoints", port)
				continue
			}
			if port.NodePort != 0 {
				instance.Status.Endpoints[port.Name] = fmt.Sprintf("%d", port.NodePort)
			} else if port.TargetPort.IntVal != 0 {
				instance.Status.Endpoints[port.Name] = fmt.Sprintf("%d", port.TargetPort.IntVal)
			} else if port.TargetPort.StrVal != "" {
				instance.Status.Endpoints[port.Name] = port.TargetPort.StrVal
			} else {
				r.Log.Info("updateStatus", "service port's targetPort is empty. Not adding it to RayCluster status.endpoints", port)
			}
		}
	} else {
		r.Log.Info("updateEndpoints", "unable to find a Service for this RayCluster. Not adding RayCluster status.endpoints", instance.Name, "Service selectors", filterLabels)
	}

	return nil
}

func (r *RayClusterReconciler) updateHeadInfo(ctx context.Context, instance *rayv1alpha1.RayCluster) error {
	if ip, err := r.getHeadPodIP(ctx, instance); err != nil {
		return err
	} else {
		instance.Status.Head.PodIP = ip
	}

	if ip, err := r.getHeadServiceIP(ctx, instance); err != nil {
		return err
	} else {
		instance.Status.Head.ServiceIP = ip
	}

	return nil
}

func (r *RayClusterReconciler) reconcileAutoscalerServiceAccount(ctx context.Context, instance *rayv1alpha1.RayCluster) error {
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	serviceAccount := &corev1.ServiceAccount{}
	namespacedName := types.NamespacedName{Namespace: instance.Namespace, Name: utils.GetHeadGroupServiceAccountName(instance)}

	if err := r.Get(ctx, namespacedName, serviceAccount); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// If users specify ServiceAccountName for the head Pod, they need to create a ServiceAccount themselves.
		// However, if KubeRay creates a ServiceAccount for users, the autoscaler may encounter permission issues during
		// zero-downtime rolling updates when RayService is performed. See https://github.com/ray-project/kuberay/issues/1123
		// for more details.
		if instance.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName == namespacedName.Name {
			r.Log.Error(err, fmt.Sprintf(
				"If users specify ServiceAccountName for the head Pod, they need to create a ServiceAccount themselves. "+
					"However, ServiceAccount %s is not found. Please create one. "+
					"See the PR description of https://github.com/ray-project/kuberay/pull/1128 for more details.", namespacedName.Name), "ServiceAccount", namespacedName)
			return err
		}

		// Create service account for autoscaler if there's no existing one in the cluster.
		serviceAccount, err := common.BuildServiceAccount(instance)
		if err != nil {
			return err
		}

		// making sure the name is valid
		serviceAccount.Name = utils.CheckName(serviceAccount.Name)

		// Set controller reference
		if err := controllerutil.SetControllerReference(instance, serviceAccount, r.Scheme); err != nil {
			return err
		}

		if err := r.Create(ctx, serviceAccount); err != nil {
			if errors.IsAlreadyExists(err) {
				r.Log.Info("Pod service account already exist, no need to create")
				return nil
			}
			r.Log.Error(err, "Pod Service Account create error!", "Pod.ServiceAccount.Error", err)
			return err
		}
		r.Log.Info("Pod ServiceAccount created successfully", "service account name", serviceAccount.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created service account %s", serviceAccount.Name)
		return nil
	}

	return nil
}

func (r *RayClusterReconciler) reconcileAutoscalerRole(ctx context.Context, instance *rayv1alpha1.RayCluster) error {
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	role := &rbacv1.Role{}
	namespacedName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}
	if err := r.Get(ctx, namespacedName, role); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// Create role for autoscaler if there's no existing one in the cluster.
		role, err := common.BuildRole(instance)
		if err != nil {
			return err
		}

		// making sure the name is valid
		role.Name = utils.CheckName(role.Name)
		// Set controller reference
		if err := controllerutil.SetControllerReference(instance, role, r.Scheme); err != nil {
			return err
		}

		if err := r.Create(ctx, role); err != nil {
			if errors.IsAlreadyExists(err) {
				r.Log.Info("role already exist, no need to create")
				return nil
			}
			r.Log.Error(err, "Role create error!", "Role.Error", err)
			return err
		}
		r.Log.Info("Role created successfully", "role name", role.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created role %s", role.Name)
		return nil
	}

	return nil
}

func (r *RayClusterReconciler) reconcileAutoscalerRoleBinding(ctx context.Context, instance *rayv1alpha1.RayCluster) error {
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	roleBinding := &rbacv1.RoleBinding{}
	namespacedName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}
	if err := r.Get(ctx, namespacedName, roleBinding); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// Create role bindings for autoscaler if there's no existing one in the cluster.
		roleBinding, err := common.BuildRoleBinding(instance)
		if err != nil {
			return err
		}

		// making sure the name is valid
		roleBinding.Name = utils.CheckName(roleBinding.Name)
		// Set controller reference
		if err := controllerutil.SetControllerReference(instance, roleBinding, r.Scheme); err != nil {
			return err
		}

		if err := r.Create(ctx, roleBinding); err != nil {
			if errors.IsAlreadyExists(err) {
				r.Log.Info("role binding already exist, no need to create")
				return nil
			}
			r.Log.Error(err, "Role binding create error!", "RoleBinding.Error", err)
			return err
		}
		r.Log.Info("RoleBinding created successfully", "role binding name", roleBinding.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created role binding %s", roleBinding.Name)
		return nil
	}

	return nil
}

func (r *RayClusterReconciler) updateClusterState(ctx context.Context, instance *rayv1alpha1.RayCluster, clusterState rayv1alpha1.ClusterState) error {
	if instance.Status.State == clusterState {
		return nil
	}
	instance.Status.State = clusterState
	r.Log.Info("updateClusterState", "Update CR Status.State", clusterState)
	return r.Status().Update(ctx, instance)
}

func (r *RayClusterReconciler) updateClusterReason(ctx context.Context, instance *rayv1alpha1.RayCluster, clusterReason string) error {
	if instance.Status.Reason == clusterReason {
		return nil
	}
	instance.Status.Reason = clusterReason
	r.Log.Info("updateClusterReason", "Update CR Status.Reason", clusterReason)
	return r.Status().Update(ctx, instance)
}
