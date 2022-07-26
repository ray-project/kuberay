package ray

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	rbacv1 "k8s.io/api/rbac/v1"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr"
	_ "k8s.io/api/apps/v1beta1"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	DefaultRequeueDuration    = 2 * time.Second
	PrioritizeWorkersToDelete bool
	ForcedClusterUpgrade      bool
)

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr manager.Manager) *RayClusterReconciler {
	return &RayClusterReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
		Recorder: mgr.GetEventRecorderFor("raycluster-controller"),
	}
}

var _ reconcile.Reconciler = &RayClusterReconciler{}

// RayClusterReconciler reconciles a RayCluster object
type RayClusterReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile reads that state of the cluster for a RayCluster object and makes changes based on it
// and what is in the RayCluster.Spec
// Automatically generate RBAC rules to allow the Controller to read and write workloads
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/finalizer,verbs=update
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
// Reconcile used to bridge the desired state with the current state
func (r *RayClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var err error

	// Try to fetch the Event instance
	event := &v1.Event{}
	if err = r.Get(context.TODO(), request.NamespacedName, event); err == nil {
		return r.eventReconcile(request, event)
	}

	// Try to fetch the RayCluster instance
	instance := &rayiov1alpha1.RayCluster{}
	if err = r.Get(context.TODO(), request.NamespacedName, instance); err == nil {
		return r.rayClusterReconcile(request, instance)
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

func (r *RayClusterReconciler) eventReconcile(request ctrl.Request, event *v1.Event) (ctrl.Result, error) {
	var unhealthyPod *corev1.Pod
	pods := corev1.PodList{}

	// we only care about pod events
	if event.InvolvedObject.Kind != "Pod" || event.Type != "Warning" || event.Reason != "Unhealthy" ||
		!strings.Contains(event.Message, "Readiness probe failed") {
		return ctrl.Result{}, nil
	}

	_ = r.Log.WithValues("event", request.NamespacedName)
	r.Log.Info("reconcile RayCluster Event", "event name", request.Name)

	if err := r.List(context.TODO(), &pods,
		client.InNamespace(event.InvolvedObject.Namespace)); err != nil {
		return ctrl.Result{}, err
	}

	for _, item := range pods.Items {
		if item.Name == event.InvolvedObject.Name {
			if unhealthyPod != nil {
				return ctrl.Result{}, fmt.Errorf("duplicate pod found")
			}
			unhealthyPod = &item
		}
	}

	if unhealthyPod == nil || unhealthyPod.Annotations == nil {
		r.Log.Info("pod not found or no valid annotations", "pod name", event.InvolvedObject.Name)
		return ctrl.Result{}, nil
	}

	if enabledString, ok := unhealthyPod.Annotations[common.RayHAEnabledAnnotationKey]; ok {
		if strings.ToLower(enabledString) != "true" {
			r.Log.Info("HA not enabled skipping event reconcile for pod.", "pod name", unhealthyPod.Name)
			return ctrl.Result{}, nil
		}
	} else {
		r.Log.Info("HAEnabled annotation not found", "pod name", unhealthyPod.Name)
		return ctrl.Result{}, nil
	}

	needUpdate := true
	if !utils.IsRunningAndReady(unhealthyPod) && unhealthyPod.Annotations != nil {
		r.Log.Info("mark pod unhealthy and need for a rebuild", "pod name", unhealthyPod.Name)
		for k, v := range unhealthyPod.GetAnnotations() {
			if k == common.RayNodeHealthStateAnnotationKey && v == common.PodUnhealthy {
				needUpdate = false
			}
		}
		if needUpdate {
			updatedPod := unhealthyPod.DeepCopy()
			updatedPod.Annotations[common.RayNodeHealthStateAnnotationKey] = common.PodUnhealthy
			if err := r.Update(context.TODO(), updatedPod); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *RayClusterReconciler) rayClusterReconcile(request ctrl.Request, instance *rayiov1alpha1.RayCluster) (ctrl.Result, error) {
	_ = r.Log.WithValues("raycluster", request.NamespacedName)
	r.Log.Info("reconciling RayCluster", "cluster name", request.Name)

	if instance.DeletionTimestamp != nil && !instance.DeletionTimestamp.IsZero() {
		r.Log.Info("RayCluster is being deleted, just ignore", "cluster name", request.Name)
		return ctrl.Result{}, nil
	}
	if err := r.reconcileAutoscalerServiceAccount(instance); err != nil {
		if updateErr := r.updateClusterState(instance, rayiov1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileAutoscalerRole(instance); err != nil {
		if updateErr := r.updateClusterState(instance, rayiov1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileAutoscalerRoleBinding(instance); err != nil {
		if updateErr := r.updateClusterState(instance, rayiov1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileIngress(instance); err != nil {
		if updateErr := r.updateClusterState(instance, rayiov1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileServices(instance, common.HeadService); err != nil {
		if updateErr := r.updateClusterState(instance, rayiov1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if common.IsAgentServiceEnabled(instance) {
		// Reconcile agent service only when enabled in annotation.
		if err := r.reconcileServices(instance, common.AgentService); err != nil {
			if updateErr := r.updateClusterState(instance, rayiov1alpha1.Failed); updateErr != nil {
				r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
			}
			return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
		}
	}
	if err := r.reconcilePods(instance); err != nil {
		if updateErr := r.updateClusterState(instance, rayiov1alpha1.Failed); updateErr != nil {
			r.Log.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	// update the status if needed
	if err := r.updateStatus(instance); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Update status not found error", "cluster name", request.Name)
		} else {
			r.Log.Error(err, "Update status error", "cluster name", request.Name)
		}
	}
	return ctrl.Result{}, nil
}

func (r *RayClusterReconciler) reconcileIngress(instance *rayiov1alpha1.RayCluster) error {
	if instance.Spec.HeadGroupSpec.EnableIngress == nil || !*instance.Spec.HeadGroupSpec.EnableIngress {
		return nil
	}

	headIngresses := networkingv1.IngressList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: instance.Name}
	if err := r.List(context.TODO(), &headIngresses, client.InNamespace(instance.Namespace), filterLabels); err != nil {
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

		err = r.createHeadIngress(ingress, instance)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RayClusterReconciler) reconcileServices(instance *rayiov1alpha1.RayCluster, serviceType common.ServiceType) error {
	services := corev1.ServiceList{}
	var filterLabels client.MatchingLabels
	if serviceType == common.HeadService {
		filterLabels = client.MatchingLabels{common.RayClusterLabelKey: instance.Name}
	} else if serviceType == common.AgentService {
		filterLabels = client.MatchingLabels{common.RayClusterDashboardServiceLabelKey: utils.GenerateDashboardAgentLabel(instance.Name)}
	}

	if err := r.List(context.TODO(), &services, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}

	if services.Items != nil {
		if len(services.Items) == 1 {
			r.Log.Info("reconcileServices ", string(serviceType)+" service found", services.Items[0].Name)
			// TODO: compare diff and reconcile the object
			// For example. ServiceType might be changed or port might be modified
			return nil
		}

		// This should never happen.
		// We add the protection here just in case controller has race issue or user manually create service with same label.
		if len(services.Items) > 1 {
			r.Log.Info("reconcileServices ", "Duplicates "+string(serviceType)+" service found", len(services.Items))
			return nil
		}
	}

	// Create head service if there's no existing one in the cluster.
	if services.Items == nil || len(services.Items) == 0 {
		var raySvc *v1.Service
		var err error
		if serviceType == common.HeadService {
			raySvc, err = common.BuildServiceForHeadPod(*instance)
		} else if serviceType == common.AgentService {
			raySvc, err = common.BuildDashboardService(*instance)
		}

		if raySvc == nil {
			r.Log.Info("reconcileServices ", "Cannot create un-support service type ", serviceType)
			return nil
		}
		if len(raySvc.Spec.Ports) == 0 {
			r.Log.Info("reconcileServices ", "Ray service has no ports set up.", raySvc.Spec)
			return nil
		}

		if err != nil {
			return err
		}

		err = r.createService(raySvc, instance)
		// if the service cannot be created we return the error and requeue
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RayClusterReconciler) reconcilePods(instance *rayiov1alpha1.RayCluster) error {
	// check if all the pods exist
	headPods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeTypeLabelKey: string(rayiov1alpha1.HeadNode)}
	if err := r.List(context.TODO(), &headPods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}
	// Reconcile head Pod
	if len(headPods.Items) == 1 {
		headPod := headPods.Items[0]
		r.Log.Info("reconcilePods ", "head pod found", headPod.Name)
		if headPod.Status.Phase == corev1.PodRunning || headPod.Status.Phase == corev1.PodPending {
			r.Log.Info("reconcilePods", "head pod is up and running... checking workers", headPod.Name)
		} else if headPod.Status.Phase == corev1.PodFailed && strings.Contains(headPod.Status.Reason, "Evicted") {
			// Handle evicted pod
			r.Log.Info("reconcilePods", "head pod has been evicted and controller needs to replace the pod", headPod.Name)
			if err := r.Delete(context.TODO(), &headPod); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("head pod %s is not running nor pending", headPod.Name)
		}
	}
	if len(headPods.Items) == 0 || headPods.Items == nil {
		// create head pod
		r.Log.Info("reconcilePods ", "creating head pod for cluster", instance.Name)
		common.CreatedClustersCounterInc(instance.Namespace)
		if err := r.createHeadPod(*instance); err != nil {
			common.FailedClustersCounterInc(instance.Namespace)
			return err
		}
		common.SuccessfulClustersCounterInc(instance.Namespace)
	} else if len(headPods.Items) > 1 {
		r.Log.Info("reconcilePods ", "more than 1 head pod found for cluster", instance.Name)
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
			if err := r.Delete(context.TODO(), &extraHeadPodToDelete); err != nil {
				return err
			}
		}
	} else {
		// we have exactly one head pod running
		if headPods.Items[0].Annotations != nil {
			if v, ok := headPods.Items[0].Annotations[common.RayNodeHealthStateAnnotationKey]; ok && v == common.PodUnhealthy {
				if err := r.Delete(context.TODO(), &headPods.Items[0]); err != nil {
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
				if err := r.Delete(context.TODO(), &headPods.Items[0]); err != nil {
					return err
				}
				return nil
			}
		}

		// check if WorkerGroupSpecs has been changed and we need to kill worker pods
		for _, worker := range instance.Spec.WorkerGroupSpecs {
			workerPods := corev1.PodList{}
			filterLabels = client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeGroupLabelKey: worker.GroupName}
			if err := r.List(context.TODO(), &workerPods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
				return err
			}
			updatedWorkerPods := false
			for _, item := range workerPods.Items {
				if utils.PodNotMatchingTemplate(item, worker.Template) {
					r.Log.Info(fmt.Sprintf("need to delete old worker pod %s", item.Name))
					if err := r.Delete(context.TODO(), &item); err != nil {
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
		workerPods := corev1.PodList{}
		filterLabels = client.MatchingLabels{common.RayClusterLabelKey: instance.Name, common.RayNodeGroupLabelKey: worker.GroupName}
		if err := r.List(context.TODO(), &workerPods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
			return err
		}

		// delete the worker pod if it is marked unhealthy
		for _, workerPod := range workerPods.Items {
			if workerPod.Annotations == nil {
				continue
			}
			if v, ok := workerPod.Annotations[common.RayNodeHealthStateAnnotationKey]; ok && v == common.PodUnhealthy {
				if err := r.Delete(context.TODO(), &workerPod); err != nil {
					return err
				}
				r.Log.Info(fmt.Sprintf("need to delete unhealthy worker pod %s", workerPod.Name))
				// we are deleting one worker pod now, let's reconcile again later
				return nil
			}
		}

		runningPods := corev1.PodList{}
		for _, aPod := range workerPods.Items {
			if (aPod.Status.Phase == corev1.PodRunning || aPod.Status.Phase == corev1.PodPending) && aPod.ObjectMeta.DeletionTimestamp == nil {
				runningPods.Items = append(runningPods.Items, aPod)
			}
		}
		r.updateLocalWorkersToDelete(&worker, runningPods.Items)
		diff := *worker.Replicas - int32(len(runningPods.Items))

		if PrioritizeWorkersToDelete {
			// Always remove the specified WorkersToDelete - regardless of the value of Replicas.
			// Essentially WorkersToDelete has to be deleted to meet the expectations of the Autoscaler.
			r.Log.Info("reconcilePods", "removing the pods in the scaleStrategy of", worker.GroupName)
			for _, podsToDelete := range worker.ScaleStrategy.WorkersToDelete {
				pod := corev1.Pod{}
				pod.Name = podsToDelete
				pod.Namespace = utils.GetNamespace(instance.ObjectMeta)
				r.Log.Info("Deleting pod", "namespace", pod.Namespace, "name", pod.Name)
				if err := r.Delete(context.TODO(), &pod); err != nil {
					if !errors.IsNotFound(err) {
						return err
					}
					r.Log.Info("reconcilePods", "unable to delete worker ", pod.Name)
				} else {
					diff++
					r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted", "Deleted pod %s", pod.Name)
				}
			}
			worker.ScaleStrategy.WorkersToDelete = []string{}
		}

		// Once we remove the feature flag and commit to those changes, the code below can be cleaned up
		// It will end being a simple: "if diff > 0 { } else { }"

		if diff > 0 {
			// pods need to be added
			r.Log.Info("reconcilePods", "add workers for group", worker.GroupName)
			// create all workers of this group
			var i int32
			for i = 0; i < diff; i++ {
				r.Log.Info("reconcilePods", "creating worker for group", worker.GroupName, fmt.Sprintf("index %d", i), fmt.Sprintf("in total %d", diff))
				if err := r.createWorkerPod(*instance, worker); err != nil {
					return err
				}
			}
		} else if diff == 0 {
			r.Log.Info("reconcilePods", "all workers already exist for group", worker.GroupName)
			continue
		} else if -diff == int32(len(worker.ScaleStrategy.WorkersToDelete)) {
			r.Log.Info("reconcilePods", "removing all the pods in the scaleStrategy of", worker.GroupName)
			for _, podsToDelete := range worker.ScaleStrategy.WorkersToDelete {
				pod := corev1.Pod{}
				pod.Name = podsToDelete
				pod.Namespace = utils.GetNamespace(instance.ObjectMeta)
				r.Log.Info("Deleting pod", "namespace", pod.Namespace, "name", pod.Name)
				if err := r.Delete(context.TODO(), &pod); err != nil {
					if !errors.IsNotFound(err) {
						return err
					}
					r.Log.Info("reconcilePods", "workers specified to delete was already deleted ", pod.Name)
				}
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted", "Deleted pod %s", pod.Name)
			}
			continue
		} else {
			// diff < 0 and not the same absolute value as int32(len(worker.ScaleStrategy.WorkersToDelete)
			// we need to scale down
			workersToRemove := int32(len(runningPods.Items)) - *worker.Replicas
			randomlyRemovedWorkers := workersToRemove - int32(len(worker.ScaleStrategy.WorkersToDelete))
			// we only need to scale down the workers in the ScaleStrategy
			r.Log.Info("reconcilePods", "removing all the pods in the scaleStrategy of", worker.GroupName)
			for _, podsToDelete := range worker.ScaleStrategy.WorkersToDelete {
				pod := corev1.Pod{}
				pod.Name = podsToDelete
				pod.Namespace = utils.GetNamespace(instance.ObjectMeta)
				r.Log.Info("Deleting pod", "namespace", pod.Namespace, "name", pod.Name)
				if err := r.Delete(context.TODO(), &pod); err != nil {
					if !errors.IsNotFound(err) {
						return err
					}
					r.Log.Info("reconcilePods", "workers specified to delete was already deleted ", pod.Name)
				}
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted", "Deleted pod %s", pod.Name)
			}

			// remove the remaining pods not part of the scaleStrategy
			i := 0
			if int(randomlyRemovedWorkers) > 0 {
				for _, randomPodToDelete := range runningPods.Items {
					found := false
					for _, podsToDelete := range worker.ScaleStrategy.WorkersToDelete {
						if randomPodToDelete.Name == podsToDelete {
							found = true
							break
						}
					}
					if !found {
						r.Log.Info("Randomly deleting pod ", "index ", i, "/", randomlyRemovedWorkers, "with name", randomPodToDelete.Name)
						if err := r.Delete(context.TODO(), &randomPodToDelete); err != nil {
							if !errors.IsNotFound(err) {
								return err
							}
							r.Log.Info("reconcilePods", "workers specified to delete was already deleted ", randomPodToDelete.Name)
						}
						r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted", "Deleted pod %s", randomPodToDelete.Name)
						// increment the number of deleted pods
						i++
						if i >= int(randomlyRemovedWorkers) {
							break
						}
					}
				}
			}
		}
	}
	return nil
}

func (r *RayClusterReconciler) updateLocalWorkersToDelete(worker *rayiov1alpha1.WorkerGroupSpec, runningItems []corev1.Pod) {
	var actualWorkersToDelete []string
	itemMap := make(map[string]int)

	// Create a map for quick lookup.
	for _, item := range runningItems {
		itemMap[item.Name] = 1
	}

	// Build actualWorkersToDelete to only include running items.
	for _, workerToDelete := range worker.ScaleStrategy.WorkersToDelete {
		if _, ok := itemMap[workerToDelete]; ok {
			actualWorkersToDelete = append(actualWorkersToDelete, workerToDelete)
		}
	}

	worker.ScaleStrategy.WorkersToDelete = actualWorkersToDelete
}

func (r *RayClusterReconciler) createHeadIngress(ingress *networkingv1.Ingress, instance *rayiov1alpha1.RayCluster) error {
	// making sure the name is valid
	ingress.Name = utils.CheckName(ingress.Name)
	if err := controllerutil.SetControllerReference(instance, ingress, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(context.TODO(), ingress); err != nil {
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

func (r *RayClusterReconciler) createService(raySvc *corev1.Service, instance *rayiov1alpha1.RayCluster) error {
	// making sure the name is valid
	raySvc.Name = utils.CheckName(raySvc.Name)
	// Set controller reference
	if err := controllerutil.SetControllerReference(instance, raySvc, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(context.TODO(), raySvc); err != nil {
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

func (r *RayClusterReconciler) createHeadPod(instance rayiov1alpha1.RayCluster) error {
	// build the pod then create it
	pod := r.buildHeadPod(instance)
	podIdentifier := types.NamespacedName{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}

	r.Log.Info("createHeadPod", "head pod with name", pod.GenerateName)
	if err := r.Create(context.TODO(), &pod); err != nil {
		if errors.IsAlreadyExists(err) {
			fetchedPod := corev1.Pod{}
			// the pod might be in terminating state, we need to check
			if errPod := r.Get(context.TODO(), podIdentifier, &fetchedPod); errPod == nil {
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

func (r *RayClusterReconciler) createWorkerPod(instance rayiov1alpha1.RayCluster, worker rayiov1alpha1.WorkerGroupSpec) error {
	// build the pod then create it
	pod := r.buildWorkerPod(instance, worker)
	podIdentifier := types.NamespacedName{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}
	replica := pod
	if err := r.Create(context.TODO(), &replica); err != nil {
		if errors.IsAlreadyExists(err) {
			fetchedPod := corev1.Pod{}
			// the pod might be in terminating state, we need to check
			if errPod := r.Get(context.TODO(), podIdentifier, &fetchedPod); errPod == nil {
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
func (r *RayClusterReconciler) buildHeadPod(instance rayiov1alpha1.RayCluster) corev1.Pod {
	podName := strings.ToLower(instance.Name + common.DashSymbol + string(rayiov1alpha1.HeadNode) + common.DashSymbol)
	podName = utils.CheckName(podName) // making sure the name is valid
	svcName := utils.GenerateServiceName(instance.Name)
	// The Ray head port used by workers to connect to the cluster (GCS server port for Ray >= 1.11.0, Redis port for older Ray.)
	headPort := common.GetHeadPort(instance.Spec.HeadGroupSpec.RayStartParams)
	autoscalingEnabled := instance.Spec.EnableInTreeAutoscaling
	podConf := common.DefaultHeadPodTemplate(instance, instance.Spec.HeadGroupSpec, podName, svcName, headPort)
	pod := common.BuildPod(podConf, rayiov1alpha1.HeadNode, instance.Spec.HeadGroupSpec.RayStartParams, svcName, headPort, autoscalingEnabled)
	// Set raycluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(&instance, &pod, r.Scheme); err != nil {
		r.Log.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

// Build worker instance pods.
func (r *RayClusterReconciler) buildWorkerPod(instance rayiov1alpha1.RayCluster, worker rayiov1alpha1.WorkerGroupSpec) corev1.Pod {
	podName := strings.ToLower(instance.Name + common.DashSymbol + string(rayiov1alpha1.WorkerNode) + common.DashSymbol + worker.GroupName + common.DashSymbol)
	podName = utils.CheckName(podName) // making sure the name is valid
	svcName := utils.GenerateServiceName(instance.Name)
	// The Ray head port used by workers to connect to the cluster (GCS server port for Ray >= 1.11.0, Redis port for older Ray.)
	headPort := common.GetHeadPort(instance.Spec.HeadGroupSpec.RayStartParams)
	autoscalingEnabled := instance.Spec.EnableInTreeAutoscaling
	podTemplateSpec := common.DefaultWorkerPodTemplate(instance, worker, podName, svcName, headPort)
	pod := common.BuildPod(podTemplateSpec, rayiov1alpha1.WorkerNode, worker.RayStartParams, svcName, headPort, autoscalingEnabled)
	// Set raycluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(&instance, &pod, r.Scheme); err != nil {
		r.Log.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

// SetupWithManager builds the reconciler.
func (r *RayClusterReconciler) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayiov1alpha1.RayCluster{}).Named("raycluster-controller").
		Watches(&source.Kind{Type: &corev1.Event{}}, &handler.EnqueueRequestForObject{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &rayiov1alpha1.RayCluster{},
		}).
		Watches(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &rayiov1alpha1.RayCluster{},
		}).
		WithOptions(controller.Options{MaxConcurrentReconciles: reconcileConcurrency}).
		Complete(r)
}

func (r *RayClusterReconciler) updateStatus(instance *rayiov1alpha1.RayCluster) error {
	runtimePods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: instance.Name}
	if err := r.List(context.TODO(), &runtimePods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}

	count := utils.CalculateAvailableReplicas(runtimePods)
	if instance.Status.AvailableWorkerReplicas != count {
		instance.Status.AvailableWorkerReplicas = count
	}

	count = utils.CalculateDesiredReplicas(instance)
	if instance.Status.DesiredWorkerReplicas != count {
		instance.Status.DesiredWorkerReplicas = count
	}

	count = utils.CalculateMinReplicas(instance)
	if instance.Status.MinWorkerReplicas != count {
		instance.Status.MinWorkerReplicas = count
	}

	count = utils.CalculateMaxReplicas(instance)
	if instance.Status.MaxWorkerReplicas != count {
		instance.Status.MaxWorkerReplicas = count
	}

	// validation for the RayStartParam for the state.
	isValid, err := common.ValidateHeadRayStartParams(instance.Spec.HeadGroupSpec)
	if err != nil {
		r.Recorder.Event(instance, corev1.EventTypeWarning, string(rayiov1alpha1.RayConfigError), err.Error())
	}
	// only in invalid status that we update the status to unhealthy.
	if !isValid {
		instance.Status.State = rayiov1alpha1.Unhealthy
	} else {
		if utils.CheckAllPodsRunnning(runtimePods) {
			instance.Status.State = rayiov1alpha1.Ready
		}
	}

	if err := r.updateEndpoints(instance); err != nil {
		return err
	}

	timeNow := metav1.Now()
	instance.Status.LastUpdateTime = &timeNow
	if err := r.Status().Update(context.Background(), instance); err != nil {
		return err
	}

	return nil
}

func (r *RayClusterReconciler) updateEndpoints(instance *rayiov1alpha1.RayCluster) error {
	// TODO: (@scarlet25151) There may be several K8s Services for a RayCluster.
	// We assume we can find the right one by filtering Services with appropriate label selectors
	// and picking the first one. We may need to select by name in the future if the Service naming is stable.
	rayHeadSvc := corev1.ServiceList{}
	filterLabels := client.MatchingLabels{
		common.RayClusterLabelKey:  instance.Name,
		common.RayNodeTypeLabelKey: "head",
	}
	if err := r.List(context.TODO(), &rayHeadSvc, client.InNamespace(instance.Namespace), filterLabels); err != nil {
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

func (r *RayClusterReconciler) reconcileAutoscalerServiceAccount(instance *rayiov1alpha1.RayCluster) error {
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	serviceAccount := &corev1.ServiceAccount{}
	namespacedName := types.NamespacedName{Namespace: instance.Namespace, Name: utils.GetHeadGroupServiceAccountName(instance)}

	if err := r.Get(context.TODO(), namespacedName, serviceAccount); err != nil {
		if !errors.IsNotFound(err) {
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

		if err := r.Create(context.TODO(), serviceAccount); err != nil {
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

func (r *RayClusterReconciler) reconcileAutoscalerRole(instance *rayiov1alpha1.RayCluster) error {
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	role := &rbacv1.Role{}
	namespacedName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}
	if err := r.Get(context.TODO(), namespacedName, role); err != nil {
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

		if err := r.Create(context.TODO(), role); err != nil {
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

func (r *RayClusterReconciler) reconcileAutoscalerRoleBinding(instance *rayiov1alpha1.RayCluster) error {
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	roleBinding := &rbacv1.RoleBinding{}
	namespacedName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}
	if err := r.Get(context.TODO(), namespacedName, roleBinding); err != nil {
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

		if err := r.Create(context.TODO(), roleBinding); err != nil {
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

func (r *RayClusterReconciler) updateClusterState(instance *rayiov1alpha1.RayCluster, clusterState rayiov1alpha1.ClusterState) error {
	instance.Status.State = clusterState
	return r.Status().Update(context.Background(), instance)
}
