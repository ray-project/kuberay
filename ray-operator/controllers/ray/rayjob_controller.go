package ray

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

const (
	RayJobDefaultRequeueDuration    = 3 * time.Second
	RayJobDefaultClusterSelectorKey = "ray.io/cluster"
)

// RayJobReconciler reconciles a RayJob object
type RayJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
}

// NewRayJobReconciler returns a new reconcile.Reconciler
func NewRayJobReconciler(mgr manager.Manager) *RayJobReconciler {
	return &RayJobReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("RayJob"),
		Recorder: mgr.GetEventRecorderFor("rayjob-controller"),
	}
}

// +kubebuilder:rbac:groups=ray.io,resources=rayjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;delete;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;delete

// [WARNING]: There MUST be a newline after kubebuilder markers.
// Reconcile reads that state of a RayJob object and makes changes based on it
// and what is in the RayJob.Spec
// Automatically generate RBAC rules to allow the Controller to read and write workloads
// Reconcile used to bridge the desired state with the current state
func (r *RayJobReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling RayJob", "NamespacedName", request.NamespacedName)
	return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
}

func (r *RayJobReconciler) deleteCluster(ctx context.Context, rayJobInstance *rayv1alpha1.RayJob) (reconcile.Result, error) {
	clusterIdentifier := types.NamespacedName{
		Name:      rayJobInstance.Status.RayClusterName,
		Namespace: rayJobInstance.Namespace,
	}
	cluster := rayv1alpha1.RayCluster{}
	if err := r.Get(ctx, clusterIdentifier, &cluster); err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
		}
		r.Log.Info("The associated cluster has been already deleted and it can not be found", "RayCluster", clusterIdentifier)
		return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
	} else {
		if cluster.DeletionTimestamp != nil {
			r.Log.Info("The cluster deletion is ongoing.", "rayjob", rayJobInstance.Name, "raycluster", cluster.Name)
		} else {
			if err := r.Delete(ctx, &cluster); err != nil {
				return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, err
			}
			r.Log.Info("The associated cluster is deleted", "RayCluster", clusterIdentifier)
			r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Deleted", "Deleted cluster %s", rayJobInstance.Status.RayClusterName)
			return ctrl.Result{RequeueAfter: RayJobDefaultRequeueDuration}, nil
		}
	}
	return ctrl.Result{}, nil
}

// isJobSucceedOrFailed indicates whether the job comes into end status.
func isJobSucceedOrFailed(status rayv1alpha1.JobStatus) bool {
	return (status == rayv1alpha1.JobStatusSucceeded) || (status == rayv1alpha1.JobStatusFailed)
}

// isJobPendingOrRunning indicates whether the job is running.
func isJobPendingOrRunning(status rayv1alpha1.JobStatus) bool {
	return (status == rayv1alpha1.JobStatusPending) || (status == rayv1alpha1.JobStatusRunning)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1alpha1.RayJob{}).
		Owns(&rayv1alpha1.RayCluster{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

func (r *RayJobReconciler) getRayJobInstance(ctx context.Context, request ctrl.Request) (*rayv1alpha1.RayJob, error) {
	rayJobInstance := &rayv1alpha1.RayJob{}
	if err := r.Get(ctx, request.NamespacedName, rayJobInstance); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Read request instance not found error!", "name", request.NamespacedName)
		} else {
			r.Log.Error(err, "Read request instance error!")
		}
		// Error reading the object - requeue the request.
		return nil, err
	}
	return rayJobInstance, nil
}

func (r *RayJobReconciler) setRayJobIdAndRayClusterNameIfNeed(ctx context.Context, rayJob *rayv1alpha1.RayJob) error {
	shouldUpdateStatus := false
	if rayJob.Status.JobId == "" {
		shouldUpdateStatus = true
		if rayJob.Spec.JobId != "" {
			rayJob.Status.JobId = rayJob.Spec.JobId
		} else {
			rayJob.Status.JobId = utils.GenerateRayJobId(rayJob.Name)
		}
	}

	if rayJob.Status.RayClusterName == "" {
		shouldUpdateStatus = true
		// if the clusterSelector is not empty, default use this cluster name
		// we assume the length of clusterSelector is one
		if len(rayJob.Spec.ClusterSelector) != 0 {
			var useValue string
			var ok bool
			if useValue, ok = rayJob.Spec.ClusterSelector[RayJobDefaultClusterSelectorKey]; !ok {
				return fmt.Errorf("failed to get cluster name in ClusterSelector map, the default key is %v", RayJobDefaultClusterSelectorKey)
			}
			rayJob.Status.RayClusterName = useValue
			rayJob.Spec.ShutdownAfterJobFinishes = false
			return nil
		} else {
			rayJob.Status.RayClusterName = utils.GenerateRayClusterName(rayJob.Name)
		}
	}

	if shouldUpdateStatus {
		return r.updateState(ctx, rayJob, nil, rayJob.Status.JobStatus, rayv1alpha1.JobDeploymentStatusInitializing, nil)
	}
	return nil
}

// make sure the priority is correct
func (r *RayJobReconciler) updateState(ctx context.Context, rayJob *rayv1alpha1.RayJob, jobInfo *utils.RayJobInfo, jobStatus rayv1alpha1.JobStatus, jobDeploymentStatus rayv1alpha1.JobDeploymentStatus, err error) error {
	// Let's skip update the APIServer if it's synced.
	if rayJob.Status.JobStatus == jobStatus && rayJob.Status.JobDeploymentStatus == jobDeploymentStatus {
		return nil
	}

	r.Log.Info("UpdateState", "oldJobStatus", rayJob.Status.JobStatus, "newJobStatus", jobStatus, "oldJobDeploymentStatus", rayJob.Status.JobDeploymentStatus, "newJobDeploymentStatus", jobDeploymentStatus)
	rayJob.Status.JobStatus = jobStatus
	rayJob.Status.JobDeploymentStatus = jobDeploymentStatus
	if jobInfo != nil {
		rayJob.Status.Message = jobInfo.Message
		rayJob.Status.StartTime = utils.ConvertUnixTimeToMetav1Time(jobInfo.StartTime)
		if jobInfo.StartTime >= jobInfo.EndTime {
			rayJob.Status.EndTime = nil
		} else {
			rayJob.Status.EndTime = utils.ConvertUnixTimeToMetav1Time(jobInfo.EndTime)
		}
	}

	// TODO (kevin85421): ObservedGeneration should be used to determine whether update this CR or not.
	rayJob.Status.ObservedGeneration = rayJob.ObjectMeta.Generation

	if errStatus := r.Status().Update(ctx, rayJob); errStatus != nil {
		return fmtErrors.Errorf("combined error: %v %v", err, errStatus)
	}
	return err
}

// TODO: select existing rayclusters by ClusterSelector
func (r *RayJobReconciler) getOrCreateRayClusterInstance(ctx context.Context, rayJobInstance *rayv1alpha1.RayJob) (*rayv1alpha1.RayCluster, error) {
	rayClusterInstanceName := rayJobInstance.Status.RayClusterName
	r.Log.V(3).Info("try to find existing RayCluster instance", "name", rayClusterInstanceName)
	rayClusterNamespacedName := types.NamespacedName{
		Namespace: rayJobInstance.Namespace,
		Name:      rayClusterInstanceName,
	}

	rayClusterInstance := &rayv1alpha1.RayCluster{}
	err := r.Get(ctx, rayClusterNamespacedName, rayClusterInstance)
	if err == nil {
		r.Log.Info("Found associated RayCluster for RayJob", "rayjob", rayJobInstance.Name, "raycluster", rayClusterNamespacedName)

		// Case1: The job is submitted to an existing ray cluster, simply return the rayClusterInstance.
		// We do not use rayJobInstance.Spec.RayClusterSpec == nil to check if the cluster selector mode is activated.
		// This is because a user might set both RayClusterSpec and ClusterSelector. with rayJobInstance.Spec.RayClusterSpec == nil,
		// though the RayJob controller will still use ClusterSelector, but it's now able to update the replica.
		// this could result in a conflict as both the RayJob controller and the autoscaler in the existing RayCluster might try to update replicas simultaneously.
		if len(rayJobInstance.Spec.ClusterSelector) != 0 {
			r.Log.Info("ClusterSelector is being used to select an existing RayCluster. RayClusterSpec will be disregarded", "raycluster", rayClusterNamespacedName)
			return rayClusterInstance, nil
		}

		// Note, unlike the RayService, which creates new Ray clusters if any spec is changed,
		// RayJob only supports changing the replicas. Changes to other specs may lead to
		// unexpected behavior. Therefore, the following code focuses solely on updating replicas.

		// Case2: In-tree autoscaling is enabled, only the autoscaler should update replicas to prevent race conditions
		// between user updates and autoscaler decisions. RayJob controller should not modify the replica. Consider this scenario:
		// 1. The autoscaler updates replicas to 10 based on the current workload.
		// 2. The user updates replicas to 15 in the RayJob YAML file.
		// 3. Both RayJob controller and the autoscaler attempt to update replicas, causing worker pods to be repeatedly created and terminated.
		if rayJobInstance.Spec.RayClusterSpec.EnableInTreeAutoscaling != nil && *rayJobInstance.Spec.RayClusterSpec.EnableInTreeAutoscaling {
			// Note, currently, there is no method to verify if the user has updated the RayJob since the last reconcile.
			// In future, we could utilize annotation that stores the hash of the RayJob since last reconcile to compare.
			// For now, we just log a warning message to remind the user regadless whether user has updated RayJob.
			r.Log.Info("Since in-tree autoscaling is enabled, any adjustments made to the RayJob will be disregarded and will not be propagated to the RayCluster.")
			return rayClusterInstance, nil
		}

		// Case3: In-tree autoscaling is disabled, respect the user's replicas setting.
		// Loop over all worker groups and update replicas.
		areReplicasIdentical := true
		for i := range rayJobInstance.Spec.RayClusterSpec.WorkerGroupSpecs {
			if *rayClusterInstance.Spec.WorkerGroupSpecs[i].Replicas != *rayJobInstance.Spec.RayClusterSpec.WorkerGroupSpecs[i].Replicas {
				areReplicasIdentical = false
				*rayClusterInstance.Spec.WorkerGroupSpecs[i].Replicas = *rayJobInstance.Spec.RayClusterSpec.WorkerGroupSpecs[i].Replicas
			}
		}

		// Other specs rather than replicas are changed, warn the user that the RayJob supports replica changes only.
		if !utils.CompareJsonStruct(rayClusterInstance.Spec, *rayJobInstance.Spec.RayClusterSpec) {
			r.Log.Info("RayJob supports replica changes only. Adjustments made to other specs will be disregarded as they may cause unexpected behavior")
		}

		// Avoid updating the RayCluster's replicas if it's identical to the RayJob's replicas.
		if areReplicasIdentical {
			return rayClusterInstance, nil
		}

		r.Log.Info("Update ray cluster replica", "raycluster", rayClusterNamespacedName)
		if err := r.Update(ctx, rayClusterInstance); err != nil {
			r.Log.Error(err, "Fail to update ray cluster replica!", "rayCluster", rayClusterNamespacedName)
			// Error updating the RayCluster object.
			return nil, client.IgnoreNotFound(err)
		}

	} else if errors.IsNotFound(err) {
		// TODO: If both ClusterSelector and RayClusterSpec are not set, we avoid should attempting to retrieve a RayCluster instance.
		// Consider moving this logic to a more appropriate location.
		if len(rayJobInstance.Spec.ClusterSelector) == 0 && rayJobInstance.Spec.RayClusterSpec == nil {
			err := fmt.Errorf("Both ClusterSelector and RayClusterSpec are undefined")
			r.Log.Error(err, "Failed to configure RayCluster instance due to missing configuration")
			return nil, err
		}

		if len(rayJobInstance.Spec.ClusterSelector) != 0 {
			err := fmt.Errorf("we have choosed the cluster selector mode, failed to find the cluster named %v, err: %v", rayClusterInstanceName, err)
			r.Log.Error(err, "Get rayCluster instance error!")
			return nil, err
		}

		// special case: is the job is complete status and cluster has been recycled.
		if isJobSucceedOrFailed(rayJobInstance.Status.JobStatus) && rayJobInstance.Status.JobDeploymentStatus == rayv1alpha1.JobDeploymentStatusComplete {
			r.Log.Info("The cluster has been recycled for the job, skip duplicate creation", "rayjob", rayJobInstance.Name)
			return nil, err
		}
		// special case: don't create a cluster instance and don't return an error if the suspend flag of the job is true
		if rayJobInstance.Spec.Suspend {
			return nil, nil
		}

		r.Log.Info("RayCluster not found, creating rayCluster!", "raycluster", rayClusterNamespacedName)
		rayClusterInstance, err = r.constructRayClusterForRayJob(rayJobInstance, rayClusterInstanceName)
		if err != nil {
			r.Log.Error(err, "unable to construct a new rayCluster")
			// Error construct the RayCluster object - requeue the request.
			return nil, err
		}
		if err := r.Create(ctx, rayClusterInstance); err != nil {
			r.Log.Error(err, "unable to create rayCluster for rayJob", "rayCluster", rayClusterInstance)
			// Error creating the RayCluster object - requeue the request.
			return nil, err
		}
		r.Log.Info("created rayCluster for rayJob", "rayCluster", rayClusterInstance)
		r.Recorder.Eventf(rayJobInstance, corev1.EventTypeNormal, "Created", "Created cluster %s", rayJobInstance.Status.RayClusterName)
	} else {
		r.Log.Error(err, "Get rayCluster instance error!")
		// Error reading the RayCluster object - requeue the request.
		return nil, err
	}

	return rayClusterInstance, nil
}

func (r *RayJobReconciler) constructRayClusterForRayJob(rayJobInstance *rayv1alpha1.RayJob, rayClusterName string) (*rayv1alpha1.RayCluster, error) {
	rayCluster := &rayv1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      rayJobInstance.Labels,
			Annotations: rayJobInstance.Annotations,
			Name:        rayClusterName,
			Namespace:   rayJobInstance.Namespace,
		},
		Spec: *rayJobInstance.Spec.RayClusterSpec.DeepCopy(),
	}

	// Set the ownership in order to do the garbage collection by k8s.
	if err := ctrl.SetControllerReference(rayJobInstance, rayCluster, r.Scheme); err != nil {
		return nil, err
	}

	return rayCluster, nil
}
