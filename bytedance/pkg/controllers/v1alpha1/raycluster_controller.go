/*
Copyright 2021 ByteDance Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"
	"fmt"
	"time"

	"github.com/ray-project/ray-contrib/bytedance/pkg/controllers/common"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayiov1alpha1 "github.com/ray-project/ray-contrib/bytedance/pkg/api/v1alpha1"
)

var (
	DefaultRequeueDuration = 2 * time.Second
)

// NewReconciler returns a new RayClusterReconciler
func NewReconciler(mgr manager.Manager) *RayClusterReconciler {
	return &RayClusterReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      mgr.GetLogger(),
		Recorder: mgr.GetEventRecorderFor(common.RayOperatorName),
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

//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ray.io,resources=rayclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ray.io,resources=rayclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *RayClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("raycluster", req.NamespacedName)
	logger.Info("Reconciling RayCluster", "cluster name", req.Name)

	// Fetch the RayCluster instance
	cluster := &rayiov1alpha1.RayCluster{}
	if err := r.Get(context.TODO(), req.NamespacedName, cluster); err != nil {
		logger.Error(err, "Read request instance error!")
		// Object not found, return.  Created objects are automatically garbage collected.
		// For additional cleanup logic use finalizers.
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// reconcile Head services
	if err := r.reconcileServices(*cluster); err != nil {
		return reconcile.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// reconcile Head and Worker nodes
	if err := r.reconcilePods(*cluster); err != nil {
		return reconcile.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// update Ray Cluster status if needed.
	r.updateStatus(cluster)
	return ctrl.Result{}, nil
}

func (r *RayClusterReconciler) reconcileServices(cluster rayiov1alpha1.RayCluster) error {
	headServices := corev1.ServiceList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: cluster.Name}
	if err := r.List(context.TODO(), &headServices, client.InNamespace(cluster.Namespace), filterLabels); err != nil {
		return err
	}

	if headServices.Items != nil && len(headServices.Items) == 1 {
		r.Log.Info("reconcileServices ", "head service found", headServices.Items[0].Name)
		// TODO: compare diff and reconcile the object
		// For example. Update ServiceType
		return nil
	}

	// Create Head Service
	if headServices.Items == nil || len(headServices.Items) == 0 {
		headSvc := r.buildHeadService(cluster)
		if err := r.createHeadService(headSvc, &cluster); err != nil {
			return nil
		}
		return nil
	}

	r.Log.Info("Never reachable. one cluster should not have two services created")
	return nil
}

func (r *RayClusterReconciler) reconcilePods(cluster rayiov1alpha1.RayCluster) error {
	// Step 1: reconcile Head pods
	headPods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: cluster.Name, common.RayNodeTypeLabelKey: string(rayiov1alpha1.HeadNode)}
	if err := r.List(context.Background(), &headPods, client.InNamespace(cluster.Namespace), filterLabels); err != nil {
		return err
	}
	if len(headPods.Items) == 1 {
		r.Log.Info("reconcilePods ", "head pod found", headPods.Items[0].Name)
		if headPods.Items[0].Status.Phase == corev1.PodRunning || headPods.Items[0].Status.Phase == corev1.PodPending {
			r.Log.Info("reconcilePods", "head pod is up and running", headPods.Items[0].Name)
		} else {
			// TODO (Jeffwan@): where to handling terminating
			return fmt.Errorf("head pod %s is not running nor pending", headPods.Items[0].Name)
		}
	}
	if headPods.Items == nil || len(headPods.Items) == 0 {
		// create head pod
		r.Log.Info("reconcilePods ", "creating head pod for cluster", cluster.Name)
		if err := r.createHeadPod(cluster); err != nil {
			return err
		}
	} else if len(headPods.Items) > 1 {
		r.Log.Info("reconcilePods ", "more than 1 head pod found for cluster", cluster.Name)
		for index := range headPods.Items {
			if headPods.Items[index].Status.Phase == corev1.PodRunning || headPods.Items[index].Status.Phase == corev1.PodPending {
				// Remove the healthy pod  at index i from the list of pods to delete
				headPods.Items[index] = headPods.Items[len(headPods.Items)-1] // replace last element with the healthy head.
				headPods.Items = headPods.Items[:len(headPods.Items)-1]       // Truncate slice.
				break                                                         // only leave 1 instance
			}
		}
		// delete all the extra head pod pods
		for _, deleteExtraHeadPod := range headPods.Items {
			if err := r.Delete(context.TODO(), &deleteExtraHeadPod); err != nil {
				return err
			}
		}
	}
	// Step 2: reconcile worker pods by group
	// TODO (Jeffwan@): consider autoscaling later
	for _, worker := range cluster.Spec.WorkerNodeSpec {
		workerPods := corev1.PodList{}
		filterLabels := client.MatchingLabels{common.RayClusterLabelKey: cluster.Name, common.RayNodeTypeLabelKey: string(rayiov1alpha1.WorkerNode), common.RayNodeGroupLabelKey: worker.NodeGroupName}
		if err := r.List(context.TODO(), &workerPods, client.InNamespace(cluster.Namespace), filterLabels); err != nil {
			return err
		}
		runningPods := corev1.PodList{}
		// TODO (jiaxin): This is not accurate because there will be short period pod is not created
		for _, aPod := range workerPods.Items {
			if aPod.Status.Phase == corev1.PodRunning || aPod.Status.Phase == corev1.PodPending {
				runningPods.Items = append(runningPods.Items, aPod)
			}
		}
		// check difference between desired and created
		diff := *worker.Replicas - int32(len(runningPods.Items))
		//pods need to be added
		if diff > 0 {
			r.Log.Info("reconcilePods", "workers needed for group", worker.NodeGroupName)
			//create all workers of this group
			var i int32
			for i = 0; i < diff; i++ {
				r.Log.Info("reconcilePods", "creating worker for group", worker.NodeGroupName, fmt.Sprint(i), fmt.Sprint(diff))
				if err := r.createWorkerPod(cluster, worker); err != nil {
					return err
				}
			}
		} else if diff == 0 {
			// diff == 0
			r.Log.Info("reconcilePods", "all workers already exist for group", worker.NodeGroupName)
			continue
		} else {
			// should not happen unless someone manually create something with exact same label.
			// TODO (Jeffwan@): Don't remove them at this moment.
			r.Log.V(1).Info("Never reachable.")
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayiov1alpha1.RayCluster{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &rayiov1alpha1.RayCluster{},
		}).
		Watches(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &rayiov1alpha1.RayCluster{},
		}).
		Complete(r)
}
