package controllers

import (
	"context"
	"fmt"
	rayiov1alpha1 "ray-operator/api/v1alpha1"
	"ray-operator/controllers/common"
	_ "ray-operator/controllers/common"
	"ray-operator/controllers/utils"
	"strings"
	"time"

	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr"
	_ "k8s.io/api/apps/v1beta1"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// K8sClient client used query K8s outside the RayClusterReconciler
var K8sClient client.Client
var log = logf.Log.WithName("raycluster-controller")

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &RayClusterReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Recorder: mgr.GetEventRecorderFor("raycluster-controller")}
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
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile used to bridge the desired state with the current state
func (r *RayClusterReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	_ = r.Log.WithValues("raycluster", request.NamespacedName)
	log.Info("Reconciling RayCluster", "cluster name", request.Name)

	// Fetch the RayCluster instance
	instance := &rayiov1alpha1.RayCluster{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)

	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		log.Error(err, "Read request instance error!")
		// Error reading the object - requeue the request.
		if !apierrs.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	rayPodSvc := common.BuildServiceForHeadPod(*instance)
	err = r.createHeadService(rayPodSvc, instance)
	// if the service cannot be created we return the error and requeue
	if err != nil {
		return reconcile.Result{}, err
	}

	if err = r.checkPods(instance, rayPodSvc.Name); err != nil {
		return reconcile.Result{RequeueAfter: 2 * time.Second}, err
		//return reconcile.Result{}, err
	}

	//update the status if needed
	r.updateStatus(instance)
	return reconcile.Result{}, nil
}

func (r *RayClusterReconciler) checkPods(instance *rayiov1alpha1.RayCluster, headSvcName string) error {
	//var updateNeeded bool
	// check if all the pods exist
	headPods := corev1.PodList{}
	if err := r.List(context.TODO(), &headPods, client.InNamespace(instance.Namespace), client.MatchingLabels{"rayClusterName": instance.Name, "groupName": "headgroup"}); err != nil {
		return err
	}
	if len(headPods.Items) == 1 {
		log.Info("checkPods ", "head pod found", headPods.Items[0].Name)
		if headPods.Items[0].Status.Phase == v1.PodRunning || headPods.Items[0].Status.Phase == v1.PodPending {
			log.Info("checkPods", "head pod is up an running... checking workers", headPods.Items[0].Name)
		} else {
			return fmt.Errorf("head pod %s is not running nor pending", headPods.Items[0].Name)
		}
	}
	if len(headPods.Items) == 0 || headPods.Items == nil {
		// create head pod
		log.Info("checkPods ", "creating head pod for cluster", instance.Name)
		if err := r.createHeadPod(*instance, headSvcName); err != nil {
			return err
		}
	} else if len(headPods.Items) > 1 {
		log.Info("checkPods ", "more than 1 head pod found for cluster", instance.Name)
		for index := range headPods.Items {
			if headPods.Items[index].Status.Phase == v1.PodRunning || headPods.Items[index].Status.Phase == v1.PodPending {
				// Remove the healthy pod  at index i from the list of pods to delete
				headPods.Items[index] = headPods.Items[len(headPods.Items)-1] // replace last element with the healthy head.
				headPods.Items = headPods.Items[:len(headPods.Items)-1]       // Truncate slice.
			}
		}
		// delete all the extra head pod pods
		for _, deleteExtraHeadPod := range headPods.Items {
			if err := r.Delete(context.TODO(), &deleteExtraHeadPod); err != nil {
				return err
			}
		}
	}
	//handle the workers now
	for index, worker := range instance.Spec.WorkerGroupsSpec {
		workerPods := corev1.PodList{}
		if err := r.List(context.TODO(), &workerPods, client.InNamespace(instance.Namespace),
			client.MatchingLabels{"rayClusterName": instance.Name, "groupName": worker.GroupName}); err != nil {
			return err
		}
		runningPods := corev1.PodList{}
		for _, aPod := range workerPods.Items {
			if aPod.Status.Phase == v1.PodRunning || aPod.Status.Phase == v1.PodPending {
				runningPods.Items = append(runningPods.Items, aPod)
			}
		}
		diff := *worker.Replicas - int32(len(runningPods.Items))
		if diff > 0 {
			//pods need to be added
			log.Info("checkPods", "workers needed for group", worker.GroupName)
			//create all workers of this group
			var i int32
			for i = 0; i < diff; i++ {
				log.Info("checkPods", "creating worker for group", worker.GroupName, fmt.Sprint(i), fmt.Sprint(diff))
				if err := r.createWorkerPod(*instance, worker, headSvcName); err != nil {
					return err
				}
			}
		} else if diff == 0 {
			log.Info("checkPods", "all workers already exist for group", worker.GroupName)
			continue
		} else if int32(len(workerPods.Items)) == (*worker.Replicas + int32(len(worker.ScaleStrategy.WorkersToDelete))) {
			log.Info("checkPods", "removing all the pods in the scaleStrategy of", worker.GroupName)
			for _, podsToDelete := range worker.ScaleStrategy.WorkersToDelete {
				pod := corev1.Pod{}
				pod.Name = podsToDelete
				pod.Namespace = utils.GetNamespace(instance.ObjectMeta)
				log.Info("Deleting pod", "namespace", pod.Namespace, "name", pod.Name)
				if err := r.Delete(context.TODO(), &pod); err != nil {
					if !errors.IsNotFound(err) {
						return err
					}
					log.Info("checkPods", "workers specified to delete was already deleted ", pod.Name)
				}
				r.Recorder.Eventf(instance, v1.EventTypeNormal, "Deleted", "Deleted pod %s", pod.Name)
			}
			instance.Spec.WorkerGroupsSpec[index].ScaleStrategy.WorkersToDelete = []string{}
		}
	}
	return nil
}

func (r *RayClusterReconciler) createHeadService(rayPodSvc *corev1.Service, instance *rayiov1alpha1.RayCluster) error {
	blockOwnerDeletion := true
	ownerReference := metav1.OwnerReference{
		APIVersion:         instance.APIVersion,
		Kind:               instance.Kind,
		Name:               instance.Name,
		UID:                instance.UID,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}
	rayPodSvc.OwnerReferences = append(rayPodSvc.OwnerReferences, ownerReference)
	if errSvc := r.Create(context.TODO(), rayPodSvc); errSvc != nil {
		if errors.IsAlreadyExists(errSvc) {
			log.Info("Pod service already exist,no need to create")
			return nil
		}
		log.Error(errSvc, "Pod Service create error!", "Pod.Service.Error", errSvc)
		return errSvc
	}
	log.Info("Pod Service created successfully", "service name", rayPodSvc.Name)
	r.Recorder.Eventf(instance, v1.EventTypeNormal, "Created", "Created service %s", rayPodSvc.Name)
	return nil
}

func (r *RayClusterReconciler) createHeadPod(instance rayiov1alpha1.RayCluster, headSvcName string) error {
	// build the pod then create it
	pod := r.buildHeadPod(instance, headSvcName)
	podIdentifier := types.NamespacedName{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}

	log.Info("createHeadPod", "head pod with name", pod.GenerateName)
	if err := r.Create(context.TODO(), &pod); err != nil {
		if errors.IsAlreadyExists(err) {
			fetchedPod := corev1.Pod{}
			// the pod might be in terminating state, we need to check
			if errPod := r.Get(context.TODO(), podIdentifier, &fetchedPod); errPod == nil {
				if fetchedPod.DeletionTimestamp != nil {
					log.Error(errPod, "create pod error!", "pod is in a terminating state, we will wait until it is cleaned up", podIdentifier)
					return err
				}
			}
			log.Info("Creating pod", "Pod already exists", pod.Name)
		} else {
			return err
		}
	}
	r.Recorder.Eventf(&instance, v1.EventTypeNormal, "Created", "Created head pod %s", pod.Name)
	return nil
}

func (r *RayClusterReconciler) createWorkerPod(instance rayiov1alpha1.RayCluster, worker rayiov1alpha1.WorkerGroupSpec, headSvcName string) error {
	// build the pod then create it
	pod := r.buildWorkerPod(instance, worker, headSvcName)
	podIdentifier := types.NamespacedName{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}
	replica := corev1.Pod{}
	replica = pod
	if err := r.Create(context.TODO(), &replica); err != nil {
		if errors.IsAlreadyExists(err) {
			fetchedPod := corev1.Pod{}
			// the pod might be in terminating state, we need to check
			if errPod := r.Get(context.TODO(), podIdentifier, &fetchedPod); errPod == nil {
				if fetchedPod.DeletionTimestamp != nil {
					log.Error(errPod, "create pod error!", "pod is in a terminating state, we will wait until it is cleaned up", podIdentifier)
					return err
				}
			}
			log.Info("Creating pod", "Pod already exists", pod.Name)
		} else {
			log.Error(fmt.Errorf("createWorkerPod error"), "error creating pod", "pod", pod)
			return err
		}
	}
	log.Info("Created pod", "Pod ", pod.Name)
	r.Recorder.Eventf(&instance, v1.EventTypeNormal, "Created", "Created worker pod %s", pod.Name)
	return nil
}

// Build head instance pod(s).
func (r *RayClusterReconciler) buildHeadPod(instance rayiov1alpha1.RayCluster, svcName string) corev1.Pod {
	podType := rayiov1alpha1.HeadNode
	podName := strings.ToLower(instance.Name + common.DashSymbol + string(rayiov1alpha1.HeadNode) + common.DashSymbol)
	podConf := common.DefaultHeadPodConfig(instance, podType, podName, svcName)
	pod := common.BuildPod(podConf, rayiov1alpha1.HeadNode, instance.Spec.HeadGroupSpec.RayStartParams, svcName)
	// Set raycluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(&instance, &pod, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

// Build worker instance pods.
func (r *RayClusterReconciler) buildWorkerPod(instance rayiov1alpha1.RayCluster, worker rayiov1alpha1.WorkerGroupSpec, svcName string) corev1.Pod {
	podType := rayiov1alpha1.WorkerNode
	podName := strings.ToLower(instance.Name + common.DashSymbol + string(podType) + common.DashSymbol + worker.GroupName + common.DashSymbol)
	podConf := common.DefaultWorkerPodConfig(instance, worker, podType, podName, svcName)
	pod := common.BuildPod(podConf, rayiov1alpha1.WorkerNode, worker.RayStartParams, svcName)
	// Set raycluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(&instance, &pod, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

// SetupWithManager builds the reconciler.
func (r *RayClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayiov1alpha1.RayCluster{}).Named("raycluster-controller").
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &rayiov1alpha1.RayCluster{},
		}).
		Complete(r)
}

func (r *RayClusterReconciler) updateStatus(instance *rayiov1alpha1.RayCluster) error {
	runtimePods := corev1.PodList{}
	if err := r.List(context.TODO(), &runtimePods, client.InNamespace(instance.Namespace), client.MatchingLabels{"rayClusterName": instance.Name}); err != nil {

	}
	count := int32(0)
	for _, pod := range runtimePods.Items {
		if pod.Status.Phase == v1.PodPending || pod.Status.Phase == v1.PodRunning {
			count++
		}
	}
	if instance.Status.AvailableReplicas != count {
		instance.Status.AvailableReplicas = count
		instance.Status.LastUpdateTime.Time = time.Now()
		if err := r.Update(context.TODO(), instance); err != nil {
			return err
		}
	}
	return nil
}
