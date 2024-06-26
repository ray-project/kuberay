package ray

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	batchv1 "k8s.io/api/batch/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"k8s.io/client-go/tools/record"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	DefaultRequeueDuration = 2 * time.Second
	EnableBatchScheduler   bool

	// Definition of a index field for pod name
	podUIDIndexField = "metadata.uid"
)

// getDiscoveryClient returns a discovery client for the current reconciler
func getDiscoveryClient(config *rest.Config) (*discovery.DiscoveryClient, error) {
	return discovery.NewDiscoveryClientForConfig(config)
}

// Check where we are running. We are trying to distinguish here whether
// this is vanilla kubernetes cluster or Openshift
func getClusterType(ctx context.Context) bool {
	logger := ctrl.LoggerFrom(ctx)
	if os.Getenv("USE_INGRESS_ON_OPENSHIFT") == "true" {
		// Environment is set to treat OpenShift cluster as Vanilla Kubernetes
		return false
	}

	// The discovery package is used to discover APIs supported by a Kubernetes API server.
	config, err := ctrl.GetConfig()
	if err == nil && config != nil {
		dclient, err := getDiscoveryClient(config)
		if err == nil && dclient != nil {
			apiGroupList, err := dclient.ServerGroups()
			if err != nil {
				logger.Info("Error while querying ServerGroups, assuming we're on Vanilla Kubernetes")
				return false
			}
			for i := 0; i < len(apiGroupList.Groups); i++ {
				if strings.HasSuffix(apiGroupList.Groups[i].Name, ".openshift.io") {
					logger.Info("We detected being on OpenShift!")
					return true
				}
			}
			return false
		}
		logger.Info("Cannot retrieve a DiscoveryClient, assuming we're on Vanilla Kubernetes")
		return false
	}
	logger.Info("Cannot retrieve config, assuming we're on Vanilla Kubernetes")
	return false
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(ctx context.Context, mgr manager.Manager, options RayClusterReconcilerOptions) *RayClusterReconciler {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, podUIDIndexField, func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{string(pod.UID)}
	}); err != nil {
		panic(err)
	}
	isOpenShift := getClusterType(ctx)

	return &RayClusterReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorderFor("raycluster-controller"),
		BatchSchedulerMgr: batchscheduler.NewSchedulerManager(mgr.GetConfig()),
		IsOpenShift:       isOpenShift,

		headSidecarContainers:   options.HeadSidecarContainers,
		workerSidecarContainers: options.WorkerSidecarContainers,
	}
}

var _ reconcile.Reconciler = &RayClusterReconciler{}

// RayClusterReconciler reconciles a RayCluster object
type RayClusterReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	BatchSchedulerMgr *batchscheduler.SchedulerManager

	headSidecarContainers   []corev1.Container
	workerSidecarContainers []corev1.Container

	IsOpenShift bool
}

type RayClusterReconcilerOptions struct {
	HeadSidecarContainers   []corev1.Container
	WorkerSidecarContainers []corev1.Container
}

// Reconcile reads that state of the cluster for a RayCluster object and makes changes based on it
// and what is in the RayCluster.Spec
// Automatically generate RBAC rules to allow the Controller to read and write workloads
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions,resources=ingresses,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;delete;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;delete

// [WARNING]: There MUST be a newline after kubebuilder markers.

// Reconcile used to bridge the desired state with the current state
func (r *RayClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	var err error

	// Try to fetch the RayCluster instance
	instance := &rayv1.RayCluster{}
	if err = r.Get(ctx, request.NamespacedName, instance); err == nil {
		return r.rayClusterReconcile(ctx, request, instance)
	}

	// No match found
	if errors.IsNotFound(err) {
		logger.Info("Read request instance not found error!")
	} else {
		logger.Error(err, "Read request instance error!")
	}
	// Error reading the object - requeue the request.
	return ctrl.Result{}, client.IgnoreNotFound(err)
}

func (r *RayClusterReconciler) deleteAllPods(ctx context.Context, filters common.AssociationOptions) (pods corev1.PodList, err error) {
	logger := ctrl.LoggerFrom(ctx)
	if err = r.List(ctx, &pods, filters.ToListOptions()...); err != nil {
		return pods, err
	}
	active := 0
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp.IsZero() {
			active++
		}
	}
	if active > 0 {
		logger.Info("Deleting all Pods with labels", "filters", filters, "Number of active Pods", active)
		return pods, r.DeleteAllOf(ctx, &corev1.Pod{}, filters.ToDeleteOptions()...)
	}
	return pods, nil
}

func (r *RayClusterReconciler) rayClusterReconcile(ctx context.Context, request ctrl.Request, instance *rayv1.RayCluster) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	// Please do NOT modify `originalRayClusterInstance` in the following code.
	originalRayClusterInstance := instance.DeepCopy()

	// The `enableGCSFTRedisCleanup` is a feature flag introduced in KubeRay v1.0.0. It determines whether
	// the Redis cleanup job should be activated. Users can disable the feature by setting the environment
	// variable `ENABLE_GCS_FT_REDIS_CLEANUP` to `false`, and undertake the Redis storage namespace cleanup
	// manually after the RayCluster CR deletion.
	enableGCSFTRedisCleanup := strings.ToLower(os.Getenv(utils.ENABLE_GCS_FT_REDIS_CLEANUP)) != "false"

	if enableGCSFTRedisCleanup && common.IsGCSFaultToleranceEnabled(*instance) {
		if instance.DeletionTimestamp.IsZero() {
			if !controllerutil.ContainsFinalizer(instance, utils.GCSFaultToleranceRedisCleanupFinalizer) {
				logger.Info(
					"GCS fault tolerance has been enabled. Implementing a finalizer to ensure that Redis is properly cleaned up once the RayCluster custom resource (CR) is deleted.",
					"finalizer", utils.GCSFaultToleranceRedisCleanupFinalizer)
				controllerutil.AddFinalizer(instance, utils.GCSFaultToleranceRedisCleanupFinalizer)
				if err := r.Update(ctx, instance); err != nil {
					err = fmt.Errorf("Failed to add the finalizer %s to the RayCluster: %w", utils.GCSFaultToleranceRedisCleanupFinalizer, err)
					return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
				}
				// Only start the RayCluster reconciliation after the finalizer is added.
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, nil
			}
		} else {
			logger.Info(
				fmt.Sprintf("The RayCluster with GCS enabled, %s, is being deleted. Start to handle the Redis cleanup finalizer %s.",
					instance.Name, utils.GCSFaultToleranceRedisCleanupFinalizer),
				"DeletionTimestamp", instance.ObjectMeta.DeletionTimestamp)

			// Delete the head Pod if it exists.
			headPods, err := r.deleteAllPods(ctx, common.RayClusterHeadPodsAssociationOptions(instance))
			if err != nil {
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
			}
			// Delete all worker Pods if they exist.
			if _, err = r.deleteAllPods(ctx, common.RayClusterWorkerPodsAssociationOptions(instance)); err != nil {
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
			}
			if len(headPods.Items) > 0 {
				logger.Info(fmt.Sprintf(
					"Wait for the head Pod %s to be terminated before initiating the Redis cleanup process. "+
						"The storage namespace %s in Redis cannot be fully deleted if the GCS process on the head Pod is still writing to it.",
					headPods.Items[0].Name, headPods.Items[0].Annotations[utils.RayExternalStorageNSAnnotationKey]))
				// Requeue after 10 seconds because it takes much longer than DefaultRequeueDuration (2 seconds) for the head Pod to be terminated.
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}

			// We can start the Redis cleanup process now because the head Pod has been terminated.
			filterLabels := client.MatchingLabels{utils.RayClusterLabelKey: instance.Name, utils.RayNodeTypeLabelKey: string(rayv1.RedisCleanupNode)}
			redisCleanupJobs := batchv1.JobList{}
			if err := r.List(ctx, &redisCleanupJobs, client.InNamespace(instance.Namespace), filterLabels); err != nil {
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
			}

			if len(redisCleanupJobs.Items) != 0 {
				// Check whether the Redis cleanup Job has been completed.
				redisCleanupJob := redisCleanupJobs.Items[0]
				logger.Info("Redis cleanup Job status", "Job name", redisCleanupJob.Name,
					"Active", redisCleanupJob.Status.Active, "Succeeded", redisCleanupJob.Status.Succeeded, "Failed", redisCleanupJob.Status.Failed)
				if condition, finished := utils.IsJobFinished(&redisCleanupJob); finished {
					controllerutil.RemoveFinalizer(instance, utils.GCSFaultToleranceRedisCleanupFinalizer)
					if err := r.Update(ctx, instance); err != nil {
						return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
					}
					switch condition {
					case batchv1.JobComplete:
						logger.Info(fmt.Sprintf(
							"The Redis cleanup Job %s has been completed. "+
								"The storage namespace %s in Redis has been fully deleted.",
							redisCleanupJob.Name, redisCleanupJob.Annotations[utils.RayExternalStorageNSAnnotationKey]))
					case batchv1.JobFailed:
						logger.Info(fmt.Sprintf(
							"The Redis cleanup Job %s has failed, requeue the RayCluster CR after 5 minute. "+
								"You should manually delete the storage namespace %s in Redis and remove the RayCluster's finalizer. "+
								"Please check https://docs.ray.io/en/master/cluster/kubernetes/user-guides/kuberay-gcs-ft.html for more details.",
							redisCleanupJob.Name, redisCleanupJob.Annotations[utils.RayExternalStorageNSAnnotationKey]))
					}
					return ctrl.Result{}, nil
				}
				// the redisCleanupJob is still running
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, nil
			}
			redisCleanupJob := r.buildRedisCleanupJob(ctx, *instance)
			if err := r.Create(ctx, &redisCleanupJob); err != nil {
				if errors.IsAlreadyExists(err) {
					logger.Info(fmt.Sprintf("Redis cleanup Job already exists. Requeue the RayCluster CR %s.", instance.Name))
					return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, nil
				}
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
			}
			logger.Info("Successfully created Redis cleanup Job", "Job name", redisCleanupJob.Name)
			return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, nil
		}
	}

	if instance.DeletionTimestamp != nil && !instance.DeletionTimestamp.IsZero() {
		logger.Info("RayCluster is being deleted, just ignore", "cluster name", request.Name)
		return ctrl.Result{}, nil
	}

	if err := r.reconcileAutoscalerServiceAccount(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1.Failed); updateErr != nil {
			logger.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	if err := r.reconcileAutoscalerRole(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1.Failed); updateErr != nil {
			logger.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileAutoscalerRoleBinding(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1.Failed); updateErr != nil {
			logger.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileIngress(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1.Failed); updateErr != nil {
			logger.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileHeadService(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1.Failed); updateErr != nil {
			logger.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	if err := r.reconcileHeadlessService(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1.Failed); updateErr != nil {
			logger.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}
	// Only reconcile the K8s service for Ray Serve when the "ray.io/enable-serve-service" annotation is set to true.
	if enableServeServiceValue, exist := instance.Annotations[utils.EnableServeServiceKey]; exist && enableServeServiceValue == utils.EnableServeServiceTrue {
		if err := r.reconcileServeService(ctx, instance); err != nil {
			if updateErr := r.updateClusterState(ctx, instance, rayv1.Failed); updateErr != nil {
				logger.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
			}
			return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
		}
	}
	if err := r.reconcilePods(ctx, instance); err != nil {
		if updateErr := r.updateClusterState(ctx, instance, rayv1.Failed); updateErr != nil {
			logger.Error(updateErr, "RayCluster update state error", "cluster name", request.Name)
		}
		if updateErr := r.updateClusterReason(ctx, instance, err.Error()); updateErr != nil {
			logger.Error(updateErr, "RayCluster update reason error", "cluster name", request.Name)
		}
		r.Recorder.Event(instance, corev1.EventTypeWarning, string(rayv1.PodReconciliationError), err.Error())
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// Calculate the new status for the RayCluster. Note that the function will deep copy `instance` instead of mutating it.
	newInstance, err := r.calculateStatus(ctx, instance)
	if err != nil {
		logger.Info("Got error when calculating new status", "cluster name", request.Name, "error", err)
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// Check if need to update the status.
	if r.inconsistentRayClusterStatus(ctx, originalRayClusterInstance.Status, newInstance.Status) {
		logger.Info("rayClusterReconcile", "Update CR status", request.Name, "status", newInstance.Status)
		if err := r.Status().Update(ctx, newInstance); err != nil {
			logger.Info("Got error when updating status", "cluster name", request.Name, "error", err, "RayCluster", newInstance)
			return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
		}
	}

	// Unconditionally requeue after the number of seconds specified in the
	// environment variable RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV. If the
	// environment variable is not set, requeue after the default value.
	requeueAfterSeconds, err := strconv.Atoi(os.Getenv(utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV))
	if err != nil {
		logger.Info(fmt.Sprintf("Environment variable %s is not set, using default value of %d seconds", utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV, utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS), "cluster name", request.Name)
		requeueAfterSeconds = utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS
	}
	logger.Info("Unconditional requeue after", "cluster name", request.Name, "seconds", requeueAfterSeconds)
	return ctrl.Result{RequeueAfter: time.Duration(requeueAfterSeconds) * time.Second}, nil
}

// Checks whether the old and new RayClusterStatus are inconsistent by comparing different fields. If the only
// differences between the old and new status are the `LastUpdateTime` and `ObservedGeneration` fields, the
// status update will not be triggered.
//
// TODO (kevin85421): The field `ObservedGeneration` is not being well-maintained at the moment. In the future,
// this field should be used to determine whether to update this CR or not.
func (r *RayClusterReconciler) inconsistentRayClusterStatus(ctx context.Context, oldStatus rayv1.RayClusterStatus, newStatus rayv1.RayClusterStatus) bool {
	logger := ctrl.LoggerFrom(ctx)
	if oldStatus.State != newStatus.State || oldStatus.Reason != newStatus.Reason {
		logger.Info("inconsistentRayClusterStatus", "detect inconsistency", fmt.Sprintf(
			"old State: %s, new State: %s, old Reason: %s, new Reason: %s",
			oldStatus.State, newStatus.State, oldStatus.Reason, newStatus.Reason))
		return true
	}
	if oldStatus.ReadyWorkerReplicas != newStatus.ReadyWorkerReplicas ||
		oldStatus.AvailableWorkerReplicas != newStatus.AvailableWorkerReplicas ||
		oldStatus.DesiredWorkerReplicas != newStatus.DesiredWorkerReplicas ||
		oldStatus.MinWorkerReplicas != newStatus.MinWorkerReplicas ||
		oldStatus.MaxWorkerReplicas != newStatus.MaxWorkerReplicas {
		logger.Info("inconsistentRayClusterStatus", "detect inconsistency", fmt.Sprintf(
			"old ReadyWorkerReplicas: %d, new ReadyWorkerReplicas: %d, "+
				"old AvailableWorkerReplicas: %d, new AvailableWorkerReplicas: %d, "+
				"old DesiredWorkerReplicas: %d, new DesiredWorkerReplicas: %d, "+
				"old MinWorkerReplicas: %d, new MinWorkerReplicas: %d, "+
				"old MaxWorkerReplicas: %d, new MaxWorkerReplicas: %d",
			oldStatus.ReadyWorkerReplicas, newStatus.ReadyWorkerReplicas,
			oldStatus.AvailableWorkerReplicas, newStatus.AvailableWorkerReplicas,
			oldStatus.DesiredWorkerReplicas, newStatus.DesiredWorkerReplicas,
			oldStatus.MinWorkerReplicas, newStatus.MinWorkerReplicas,
			oldStatus.MaxWorkerReplicas, newStatus.MaxWorkerReplicas))
		return true
	}
	if !reflect.DeepEqual(oldStatus.Endpoints, newStatus.Endpoints) || !reflect.DeepEqual(oldStatus.Head, newStatus.Head) {
		logger.Info("inconsistentRayClusterStatus", "detect inconsistency", fmt.Sprintf(
			"old Endpoints: %v, new Endpoints: %v, old Head: %v, new Head: %v",
			oldStatus.Endpoints, newStatus.Endpoints, oldStatus.Head, newStatus.Head))
		return true
	}
	return false
}

func (r *RayClusterReconciler) reconcileIngress(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("Reconciling Ingress")
	if instance.Spec.HeadGroupSpec.EnableIngress == nil || !*instance.Spec.HeadGroupSpec.EnableIngress {
		return nil
	}

	if r.IsOpenShift {
		// This is open shift - create route
		return r.reconcileRouteOpenShift(ctx, instance)
	}
	// plain vanilla kubernetes - create ingress
	return r.reconcileIngressKubernetes(ctx, instance)
}

func (r *RayClusterReconciler) reconcileRouteOpenShift(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	headRoutes := routev1.RouteList{}
	filterLabels := client.MatchingLabels{utils.RayClusterLabelKey: instance.Name}
	if err := r.List(ctx, &headRoutes, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}

	if headRoutes.Items != nil && len(headRoutes.Items) == 1 {
		logger.Info("reconcileIngresses", "head service route found", headRoutes.Items[0].Name)
		return nil
	}

	if headRoutes.Items == nil || len(headRoutes.Items) == 0 {
		route, err := common.BuildRouteForHeadService(*instance)
		if err != nil {
			return err
		}

		if err := ctrl.SetControllerReference(instance, route, r.Scheme); err != nil {
			return err
		}

		err = r.createHeadRoute(ctx, route, instance)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RayClusterReconciler) reconcileIngressKubernetes(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	headIngresses := networkingv1.IngressList{}
	filterLabels := client.MatchingLabels{utils.RayClusterLabelKey: instance.Name}
	if err := r.List(ctx, &headIngresses, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}

	if headIngresses.Items != nil && len(headIngresses.Items) == 1 {
		logger.Info("reconcileIngresses", "head service ingress found", headIngresses.Items[0].Name)
		return nil
	}

	if headIngresses.Items == nil || len(headIngresses.Items) == 0 {
		ingress, err := common.BuildIngressForHeadService(ctx, *instance)
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
func (r *RayClusterReconciler) reconcileHeadService(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	services := corev1.ServiceList{}
	filterLabels := client.MatchingLabels{utils.RayClusterLabelKey: instance.Name, utils.RayNodeTypeLabelKey: string(rayv1.HeadNode)}

	if err := r.List(ctx, &services, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return err
	}

	// Check if there's existing head service in the cluster.
	if len(services.Items) != 0 {
		if len(services.Items) == 1 {
			logger.Info("reconcileHeadService", "1 head service found", services.Items[0].Name)
			return nil
		}
		// This should never happen. This protects against the case that users manually create service with the same label.
		if len(services.Items) > 1 {
			logger.Info("reconcileHeadService", "Duplicate head service found", services.Items)
			return fmt.Errorf("%d head service found %v", len(services.Items), services.Items)
		}
	} else {
		// Create head service if there's no existing one in the cluster.
		labels := make(map[string]string)
		if val, ok := instance.Spec.HeadGroupSpec.Template.ObjectMeta.Labels[utils.KubernetesApplicationNameLabelKey]; ok {
			labels[utils.KubernetesApplicationNameLabelKey] = val
		}
		annotations := make(map[string]string)
		// TODO (kevin85421): KubeRay has already exposed the entire head service (#1040) to users.
		// We may consider deprecating this field when we bump the CRD version.
		for k, v := range instance.Spec.HeadServiceAnnotations {
			annotations[k] = v
		}
		headSvc, err := common.BuildServiceForHeadPod(ctx, *instance, labels, annotations)
		// TODO (kevin85421): Provide a detailed and actionable error message. For example, which port is missing?
		if len(headSvc.Spec.Ports) == 0 {
			logger.Info("Ray head service does not have any ports set up. Service specification: %v", headSvc.Spec)
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

// Return nil only when the serve service successfully created or already exists.
func (r *RayClusterReconciler) reconcileServeService(ctx context.Context, instance *rayv1.RayCluster) error {
	// Retrieve the Service from the Kubernetes cluster with the name and namespace.
	svc := &corev1.Service{}
	err := r.Get(ctx, common.RayClusterServeServiceNamespacedName(instance), svc)
	if err == nil {
		// service exists, do nothing
		return nil
	} else if errors.IsNotFound(err) {
		// Service does not exist, create it
		svc, err = common.BuildServeServiceForRayCluster(ctx, *instance)
		if err != nil {
			return err
		}
		// Set the ownwer reference
		if err := ctrl.SetControllerReference(instance, svc, r.Scheme); err != nil {
			return err
		}
		// create service
		return r.Create(ctx, svc)
	}
	return err
}

// Return nil only when the headless service for multi-host worker groups is successfully created or already exists.
func (r *RayClusterReconciler) reconcileHeadlessService(ctx context.Context, instance *rayv1.RayCluster) error {
	// Check if there are worker groups with NumOfHosts > 1 in the cluster
	isMultiHost := false
	for _, workerGroup := range instance.Spec.WorkerGroupSpecs {
		if workerGroup.NumOfHosts > 1 {
			isMultiHost = true
			break
		}
	}

	if isMultiHost {
		services := corev1.ServiceList{}
		options := common.RayClusterHeadlessServiceListOptions(instance)

		if err := r.List(ctx, &services, options...); err != nil {
			return err
		}
		// Check if there's an existing headless service in the cluster.
		if len(services.Items) != 0 {
			// service exists, do nothing
			return nil
		}
		// Create headless tpu worker service if there's no existing one in the cluster.
		headlessSvc, err := common.BuildHeadlessServiceForRayCluster(*instance)
		if err != nil {
			return err
		}

		if err := r.createService(ctx, headlessSvc, instance); err != nil {
			return err
		}
	}

	return nil
}

func (r *RayClusterReconciler) reconcilePods(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// if RayCluster is suspended, delete all pods and skip reconcile
	if instance.Spec.Suspend != nil && *instance.Spec.Suspend {
		if _, err := r.deleteAllPods(ctx, common.RayClusterAllPodsAssociationOptions(instance)); err != nil {
			return err
		}

		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted",
			"Deleted Pods for RayCluster %s/%s due to suspension",
			instance.Namespace, instance.Name)
		return nil
	}

	// check if all the pods exist
	headPods := corev1.PodList{}
	if err := r.List(ctx, &headPods, common.RayClusterHeadPodsAssociationOptions(instance).ToListOptions()...); err != nil {
		return err
	}
	if EnableBatchScheduler {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(instance); err == nil {
			if err := scheduler.DoBatchSchedulingOnSubmission(ctx, instance); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Reconcile head Pod
	if len(headPods.Items) == 1 {
		headPod := headPods.Items[0]
		logger.Info("reconcilePods", "Found 1 head Pod", headPod.Name, "Pod status", headPod.Status.Phase,
			"Pod restart policy", headPod.Spec.RestartPolicy,
			"Ray container terminated status", getRayContainerStateTerminated(headPod))

		shouldDelete, reason := shouldDeletePod(headPod, rayv1.HeadNode)
		logger.Info("reconcilePods", "head Pod", headPod.Name, "shouldDelete", shouldDelete, "reason", reason)
		if shouldDelete {
			if err := r.Delete(ctx, &headPod); err != nil {
				return err
			}
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted",
				"Deleted head Pod %s; Pod status: %s; Pod restart policy: %s; Ray container terminated status: %v",
				headPod.Name, headPod.Status.Phase, headPod.Spec.RestartPolicy, getRayContainerStateTerminated(headPod))
			return fmt.Errorf(reason)
		}
	} else if len(headPods.Items) == 0 {
		// Create head Pod if it does not exist.
		logger.Info("reconcilePods", "Found 0 head Pods; creating a head Pod for the RayCluster.", instance.Name)
		common.CreatedClustersCounterInc(instance.Namespace)
		if err := r.createHeadPod(ctx, *instance); err != nil {
			common.FailedClustersCounterInc(instance.Namespace)
			return err
		}
		common.SuccessfulClustersCounterInc(instance.Namespace)
	} else if len(headPods.Items) > 1 {
		logger.Info("reconcilePods", fmt.Sprintf("Found %d head Pods; deleting extra head Pods.", len(headPods.Items)), instance.Name)
		// TODO (kevin85421): In-place update may not be a good idea.
		itemLength := len(headPods.Items)
		for index := 0; index < itemLength; index++ {
			if headPods.Items[index].Status.Phase == corev1.PodRunning || headPods.Items[index].Status.Phase == corev1.PodPending {
				// Remove the healthy pod at index i from the list of pods to delete
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
	}

	// Reconcile worker pods now
	for _, worker := range instance.Spec.WorkerGroupSpecs {
		// workerReplicas will store the target number of pods for this worker group.
		var workerReplicas int32 = utils.GetWorkerGroupDesiredReplicas(ctx, worker)
		logger.Info("reconcilePods", "desired workerReplicas (always adhering to minReplicas/maxReplica)", workerReplicas, "worker group", worker.GroupName, "maxReplicas", worker.MaxReplicas, "minReplicas", worker.MinReplicas, "replicas", worker.Replicas)

		workerPods := corev1.PodList{}
		if err := r.List(ctx, &workerPods, common.RayClusterGroupPodsAssociationOptions(instance, worker.GroupName).ToListOptions()...); err != nil {
			return err
		}

		// Delete unhealthy worker Pods.
		deletedWorkers := make(map[string]struct{})
		deleted := struct{}{}
		numDeletedUnhealthyWorkerPods := 0
		for _, workerPod := range workerPods.Items {
			shouldDelete, reason := shouldDeletePod(workerPod, rayv1.WorkerNode)
			logger.Info("reconcilePods", "worker Pod", workerPod.Name, "shouldDelete", shouldDelete, "reason", reason)
			if shouldDelete {
				numDeletedUnhealthyWorkerPods++
				deletedWorkers[workerPod.Name] = deleted
				if err := r.Delete(ctx, &workerPod); err != nil {
					return err
				}
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted",
					"Deleted worker Pod %s; Pod status: %s; Pod restart policy: %s; Ray container terminated status: %v",
					workerPod.Name, workerPod.Status.Phase, workerPod.Spec.RestartPolicy, getRayContainerStateTerminated(workerPod))
			}
		}

		// If we delete unhealthy Pods, we will not create new Pods in this reconciliation.
		if numDeletedUnhealthyWorkerPods > 0 {
			return fmt.Errorf("Delete %d unhealthy worker Pods", numDeletedUnhealthyWorkerPods)
		}

		// Always remove the specified WorkersToDelete - regardless of the value of Replicas.
		// Essentially WorkersToDelete has to be deleted to meet the expectations of the Autoscaler.
		logger.Info("reconcilePods", "removing the pods in the scaleStrategy of", worker.GroupName)
		for _, podsToDelete := range worker.ScaleStrategy.WorkersToDelete {
			pod := corev1.Pod{}
			pod.Name = podsToDelete
			pod.Namespace = utils.GetNamespace(instance.ObjectMeta)
			logger.Info("Deleting pod", "namespace", pod.Namespace, "name", pod.Name)
			if err := r.Delete(ctx, &pod); err != nil {
				if !errors.IsNotFound(err) {
					logger.Info("reconcilePods", "Fail to delete Pod", pod.Name, "error", err)
					return err
				}
				logger.Info("reconcilePods", "The worker Pod has already been deleted", pod.Name)
			} else {
				deletedWorkers[pod.Name] = deleted
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted", "Deleted pod %s", pod.Name)
			}
		}
		worker.ScaleStrategy.WorkersToDelete = []string{}

		runningPods := corev1.PodList{}
		for _, pod := range workerPods.Items {
			if _, ok := deletedWorkers[pod.Name]; !ok {
				runningPods.Items = append(runningPods.Items, pod)
			}
		}
		// A replica can contain multiple hosts, so we need to calculate this based on the number of hosts per replica.
		// If the user doesn't install the CRD with `NumOfHosts`, the zero value of `NumOfHosts`, which is 0, will be used.
		// Hence, all workers will be deleted. Here, we set `NumOfHosts` to max(1, `NumOfHosts`) to avoid this situation.
		if worker.NumOfHosts <= 0 {
			worker.NumOfHosts = 1
		}
		numExpectedPods := workerReplicas * worker.NumOfHosts
		diff := numExpectedPods - int32(len(runningPods.Items))

		logger.Info("reconcilePods", "workerReplicas", workerReplicas, "NumOfHosts", worker.NumOfHosts, "runningPods", len(runningPods.Items), "diff", diff)

		if diff > 0 {
			// pods need to be added
			logger.Info("reconcilePods", "Number workers to add", diff, "Worker group", worker.GroupName)
			// create all workers of this group
			var i int32
			for i = 0; i < diff; i++ {
				logger.Info("reconcilePods", "creating worker for group", worker.GroupName, fmt.Sprintf("index %d", i), fmt.Sprintf("in total %d", diff))
				if err := r.createWorkerPod(ctx, *instance, *worker.DeepCopy()); err != nil {
					return err
				}
			}
		} else if diff == 0 {
			logger.Info("reconcilePods", "all workers already exist for group", worker.GroupName)
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
				if s := os.Getenv(utils.ENABLE_RANDOM_POD_DELETE); strings.ToLower(s) == "true" {
					enableRandomPodDelete = true
				}
			}
			// Case 1: If Autoscaler is disabled, we will always enable random Pod deletion no matter the value of the feature flag.
			// Case 2: If Autoscaler is enabled, we will respect the value of the feature flag. If the feature flag environment variable
			// is not set, we will disable random Pod deletion by default.
			if !enableInTreeAutoscaling || enableRandomPodDelete {
				// diff < 0 means that we need to delete some Pods to meet the desired number of replicas.
				randomlyRemovedWorkers := -diff
				logger.Info("reconcilePods", "Number workers to delete randomly", randomlyRemovedWorkers, "Worker group", worker.GroupName)
				for i := 0; i < int(randomlyRemovedWorkers); i++ {
					randomPodToDelete := runningPods.Items[i]
					logger.Info("Randomly deleting Pod", "progress", fmt.Sprintf("%d / %d", i+1, randomlyRemovedWorkers), "with name", randomPodToDelete.Name)
					if err := r.Delete(ctx, &randomPodToDelete); err != nil {
						if !errors.IsNotFound(err) {
							return err
						}
						logger.Info("reconcilePods", "The worker Pod has already been deleted", randomPodToDelete.Name)
					}
					r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Deleted", "Deleted Pod %s", randomPodToDelete.Name)
				}
			} else {
				logger.Info(fmt.Sprintf("Random Pod deletion is disabled for cluster %s. The only decision-maker for Pod deletions is Autoscaler.", instance.Name))
			}
		}
	}
	return nil
}

// shouldDeletePod returns whether the Pod should be deleted and the reason
//
// @param pod: The Pod to be checked.
// @param nodeType: The type of the node that the Pod belongs to (head or worker).
//
// @return: shouldDelete (bool), reason (string)
// (1) shouldDelete: Whether the Pod should be deleted.
// (2) reason: The reason why the Pod should or should not be deleted.
func shouldDeletePod(pod corev1.Pod, nodeType rayv1.RayNodeType) (bool, string) {
	// If a Pod's restart policy is set to `Always`, KubeRay will not delete
	// the Pod and rely on the Pod's restart policy to restart the Pod.
	isRestartPolicyAlways := pod.Spec.RestartPolicy == corev1.RestartPolicyAlways

	// If the Pod's status is `Failed` or `Succeeded`, the Pod will not restart and we can safely delete it.
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		if isRestartPolicyAlways {
			// Based on my observation, a Pod with `RestartPolicy: Always` will never be in the terminated states (i.e., `Failed` or `Succeeded`).
			// However, I couldn't find any well-defined behavior in the Kubernetes documentation, so I can't guarantee that the status transition
			// from `Running` to `Failed / Succeeded` and back to `Running` won't occur when we kill the main process (i.e., `ray start` in KubeRay)
			// in the head Pod. Therefore, I've added this check as a safeguard.
			reason := fmt.Sprintf(
				"The status of the %s Pod %s is %s. However, KubeRay will not delete the Pod because its restartPolicy is set to 'Always' "+
					"and it should be able to restart automatically.", nodeType, pod.Name, pod.Status.Phase)
			return false, reason
		}

		reason := fmt.Sprintf(
			"The %s Pod %s status is %s which is a terminal state and it will not restart. "+
				"KubeRay will delete the Pod and create new Pods in the next reconciliation if necessary.", nodeType, pod.Name, pod.Status.Phase)
		return true, reason
	}

	rayContainerTerminated := getRayContainerStateTerminated(pod)
	if pod.Status.Phase == corev1.PodRunning && rayContainerTerminated != nil {
		if isRestartPolicyAlways {
			// If restart policy is set to `Always`, KubeRay will not delete the Pod.
			reason := fmt.Sprintf(
				"The Pod status of the %s Pod %s is %s, and the Ray container terminated status is %v. However, KubeRay will not delete the Pod because its restartPolicy is set to 'Always' "+
					"and it should be able to restart automatically.", nodeType, pod.Name, pod.Status.Phase, rayContainerTerminated)
			return false, reason
		}
		reason := fmt.Sprintf(
			"The Pod status of the %s Pod %s is %s, and the Ray container terminated status is %v. "+
				"The container is unable to restart due to its restart policy %s, so KubeRay will delete it.",
			nodeType, pod.Name, pod.Status.Phase, rayContainerTerminated, pod.Spec.RestartPolicy)
		return true, reason
	}

	// TODO (kevin85421): Consider deleting a Pod if its Ray container restarts excessively, as this might
	// suggest an unhealthy Kubernetes node. Deleting and then recreating the Pod might allow it to be
	// scheduled on a different node.
	//
	// (1) Head Pod:
	// It's aggressive to delete a head Pod that is not in a terminated state (i.e., `Failed` or `Succeeded`).
	// We should only delete a head Pod when GCS fault tolerance is enabled, and drain the head Pod before
	// deleting it.
	//
	// (2) Worker Pod:
	// Compared to deleting a head Pod, removing a worker Pod is less aggressive and aligns more closely with
	// the behavior of the Ray Autoscaler. Nevertheless, we should still carefully drain the node before deleting
	// the worker Pod. Enabling GCS fault tolerance might not be necessary when deleting worker Pods. Note that
	// the Ray Autoscaler will not delete any worker Pods that have never been registered with the Ray cluster.
	// Therefore, we may need to address the Ray Autoscaler's blind spots.

	reason := fmt.Sprintf(
		"KubeRay does not need to delete the %s Pod %s. The Pod status is %s, and the Ray container terminated status is %v.",
		nodeType, pod.Name, pod.Status.Phase, rayContainerTerminated)
	return false, reason
}

// `ContainerStatuses` does not guarantee the order of the containers. Therefore, we need to find the Ray
// container's status by name. See the following links for more details:
// (1) https://discuss.kubernetes.io/t/pod-spec-containers-and-pod-status-containerstatuses-can-have-a-different-order-why/25273
// (2) https://github.com/kubernetes/kubernetes/blob/03762cbcb52b2a4394e4d795f9d3517a78a5e1a2/pkg/api/v1/pod/util.go#L261-L268
func getRayContainerStateTerminated(pod corev1.Pod) *corev1.ContainerStateTerminated {
	rayContainerName := pod.Spec.Containers[utils.RayContainerIndex].Name
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == rayContainerName {
			return containerStatus.State.Terminated
		}
	}
	// If the Ray container isn't found, we'll assume it hasn't terminated. This scenario
	// typically arises during testing (`raycluster_controller_test.go`) as `envtest` lacks
	// a Pod controller, preventing automatic Pod status updates.
	return nil
}

func (r *RayClusterReconciler) createHeadIngress(ctx context.Context, ingress *networkingv1.Ingress, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// making sure the name is valid
	ingress.Name = utils.CheckName(ingress.Name)
	if err := controllerutil.SetControllerReference(instance, ingress, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, ingress); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Ingress already exists, no need to create")
			return nil
		}
		return err
	}
	logger.Info("Ingress created successfully", "ingress name", ingress.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created ingress %s", ingress.Name)
	return nil
}

func (r *RayClusterReconciler) createHeadRoute(ctx context.Context, route *routev1.Route, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// making sure the name is valid
	route.Name = utils.CheckRouteName(ctx, route.Name, route.Namespace)

	if err := r.Create(ctx, route); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Route already exists, no need to create")
			return nil
		}
		return err
	}
	logger.Info("Route created successfully", "route name", route.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created route %s", route.Name)
	return nil
}

func (r *RayClusterReconciler) createService(ctx context.Context, raySvc *corev1.Service, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// making sure the name is valid
	raySvc.Name = utils.CheckName(raySvc.Name)
	// Set controller reference
	if err := controllerutil.SetControllerReference(instance, raySvc, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, raySvc); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Pod service already exist, no need to create")
			return nil
		}
		return err
	}
	logger.Info("Pod Service created successfully", "service name", raySvc.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created service %s", raySvc.Name)
	return nil
}

func (r *RayClusterReconciler) createHeadPod(ctx context.Context, instance rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// build the pod then create it
	pod := r.buildHeadPod(ctx, instance)
	if EnableBatchScheduler {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(&instance); err == nil {
			scheduler.AddMetadataToPod(&instance, utils.RayNodeHeadGroupLabelValue, &pod)
		} else {
			return err
		}
	}

	logger.Info("createHeadPod", "head pod with name", pod.GenerateName)
	if err := r.Create(ctx, &pod); err != nil {
		return err
	}
	r.Recorder.Eventf(&instance, corev1.EventTypeNormal, "Created", "Created head pod %s", pod.Name)
	return nil
}

func (r *RayClusterReconciler) createWorkerPod(ctx context.Context, instance rayv1.RayCluster, worker rayv1.WorkerGroupSpec) error {
	logger := ctrl.LoggerFrom(ctx)

	// build the pod then create it
	pod := r.buildWorkerPod(ctx, instance, worker)
	if EnableBatchScheduler {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(&instance); err == nil {
			scheduler.AddMetadataToPod(&instance, worker.GroupName, &pod)
		} else {
			return err
		}
	}

	replica := pod
	if err := r.Create(ctx, &replica); err != nil {
		return err
	}
	logger.Info("Created pod", "Pod ", pod.GenerateName)
	r.Recorder.Eventf(&instance, corev1.EventTypeNormal, "Created", "Created worker pod %s", pod.Name)
	return nil
}

// Build head instance pod(s).
func (r *RayClusterReconciler) buildHeadPod(ctx context.Context, instance rayv1.RayCluster) corev1.Pod {
	logger := ctrl.LoggerFrom(ctx)
	podName := strings.ToLower(instance.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol)
	podName = utils.CheckName(podName)                                            // making sure the name is valid
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, instance, instance.Namespace) // Fully Qualified Domain Name
	// The Ray head port used by workers to connect to the cluster (GCS server port for Ray >= 1.11.0, Redis port for older Ray.)
	headPort := common.GetHeadPort(instance.Spec.HeadGroupSpec.RayStartParams)
	autoscalingEnabled := instance.Spec.EnableInTreeAutoscaling
	podConf := common.DefaultHeadPodTemplate(ctx, instance, instance.Spec.HeadGroupSpec, podName, headPort)
	if len(r.headSidecarContainers) > 0 {
		podConf.Spec.Containers = append(podConf.Spec.Containers, r.headSidecarContainers...)
	}
	logger.Info("head pod labels", "labels", podConf.Labels)
	creatorCRDType := getCreatorCRDType(instance)
	pod := common.BuildPod(ctx, podConf, rayv1.HeadNode, instance.Spec.HeadGroupSpec.RayStartParams, headPort, autoscalingEnabled, creatorCRDType, fqdnRayIP)
	// Set raycluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(&instance, &pod, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

func getCreatorCRDType(instance rayv1.RayCluster) utils.CRDType {
	return utils.GetCRDType(instance.Labels[utils.RayOriginatedFromCRDLabelKey])
}

// Build worker instance pods.
func (r *RayClusterReconciler) buildWorkerPod(ctx context.Context, instance rayv1.RayCluster, worker rayv1.WorkerGroupSpec) corev1.Pod {
	logger := ctrl.LoggerFrom(ctx)
	podName := strings.ToLower(instance.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol)
	podName = utils.CheckName(podName)                                            // making sure the name is valid
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, instance, instance.Namespace) // Fully Qualified Domain Name

	// The Ray head port used by workers to connect to the cluster (GCS server port for Ray >= 1.11.0, Redis port for older Ray.)
	headPort := common.GetHeadPort(instance.Spec.HeadGroupSpec.RayStartParams)
	autoscalingEnabled := instance.Spec.EnableInTreeAutoscaling
	podTemplateSpec := common.DefaultWorkerPodTemplate(ctx, instance, worker, podName, fqdnRayIP, headPort)
	if len(r.workerSidecarContainers) > 0 {
		podTemplateSpec.Spec.Containers = append(podTemplateSpec.Spec.Containers, r.workerSidecarContainers...)
	}
	creatorCRDType := getCreatorCRDType(instance)
	pod := common.BuildPod(ctx, podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, headPort, autoscalingEnabled, creatorCRDType, fqdnRayIP)
	// Set raycluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(&instance, &pod, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

func (r *RayClusterReconciler) buildRedisCleanupJob(ctx context.Context, instance rayv1.RayCluster) batchv1.Job {
	logger := ctrl.LoggerFrom(ctx)

	pod := r.buildHeadPod(ctx, instance)
	pod.Labels[utils.RayNodeTypeLabelKey] = string(rayv1.RedisCleanupNode)

	// Only keep the Ray container in the Redis cleanup Job.
	pod.Spec.Containers = []corev1.Container{pod.Spec.Containers[utils.RayContainerIndex]}
	pod.Spec.Containers[utils.RayContainerIndex].Command = []string{"/bin/bash", "-lc", "--"}
	pod.Spec.Containers[utils.RayContainerIndex].Args = []string{
		"echo \"To get more information about manually delete the storage namespace in Redis and remove the RayCluster's finalizer, please check https://docs.ray.io/en/master/cluster/kubernetes/user-guides/kuberay-gcs-ft.html for more details.\" && " +
			"python -c " +
			"\"from ray._private.gcs_utils import cleanup_redis_storage; " +
			"from urllib.parse import urlparse; " +
			"import os; " +
			"import sys; " +
			"redis_address = os.getenv('RAY_REDIS_ADDRESS', '').split(',')[0]; " +
			"redis_address = redis_address if '://' in redis_address else 'redis://' + redis_address; " +
			"parsed = urlparse(redis_address); " +
			"sys.exit(1) if not cleanup_redis_storage(host=parsed.hostname, port=parsed.port, password=os.getenv('REDIS_PASSWORD', parsed.password), use_ssl=parsed.scheme=='rediss', storage_namespace=os.getenv('RAY_external_storage_namespace')) else None\"",
	}

	// Disable liveness and readiness probes because the Job will not launch processes like Raylet and GCS.
	pod.Spec.Containers[utils.RayContainerIndex].LivenessProbe = nil
	pod.Spec.Containers[utils.RayContainerIndex].ReadinessProbe = nil

	// Set the environment variables to ensure that the cleanup Job has at least 60s.
	pod.Spec.Containers[utils.RayContainerIndex].Env = append(pod.Spec.Containers[utils.RayContainerIndex].Env, corev1.EnvVar{
		Name:  "RAY_redis_db_connect_retries",
		Value: "120",
	})
	pod.Spec.Containers[utils.RayContainerIndex].Env = append(pod.Spec.Containers[utils.RayContainerIndex].Env, corev1.EnvVar{
		Name:  "RAY_redis_db_connect_wait_milliseconds",
		Value: "500",
	})

	// The container's resource consumption remains constant. so hard-coding the resources is acceptable.
	// In addition, avoid using the GPU for the Redis cleanup Job.
	pod.Spec.Containers[utils.RayContainerIndex].Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}

	// For Kubernetes Job, the valid values for Pod's `RestartPolicy` are `Never` and `OnFailure`.
	pod.Spec.RestartPolicy = corev1.RestartPolicyNever

	redisCleanupJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-%s", instance.Name, "redis-cleanup"),
			Namespace:   instance.Namespace,
			Labels:      pod.Labels,
			Annotations: pod.Annotations,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To[int32](0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: pod.ObjectMeta,
				Spec:       pod.Spec,
			},
			// make this job be best-effort only for 5 minutes.
			ActiveDeadlineSeconds: ptr.To[int64](300),
		},
	}

	if err := controllerutil.SetControllerReference(&instance, &redisCleanupJob, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for the Redis cleanup Job.")
	}

	return redisCleanupJob
}

// SetupWithManager builds the reconciler.
func (r *RayClusterReconciler) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayCluster{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{})

	if EnableBatchScheduler {
		b = batchscheduler.ConfigureReconciler(b)
	}

	return b.
		WithOptions(controller.Options{
			MaxConcurrentReconciles: reconcileConcurrency,
			LogConstructor: func(request *reconcile.Request) logr.Logger {
				logger := ctrl.Log.WithName("controllers").WithName("RayCluster")
				if request != nil {
					logger = logger.WithValues("RayCluster", request.NamespacedName)
				}
				return logger
			},
		}).
		Complete(r)
}

func (r *RayClusterReconciler) calculateStatus(ctx context.Context, instance *rayv1.RayCluster) (*rayv1.RayCluster, error) {
	// Deep copy the instance, so we don't mutate the original object.
	newInstance := instance.DeepCopy()

	// TODO (kevin85421): ObservedGeneration should be used to determine whether to update this CR or not.
	newInstance.Status.ObservedGeneration = newInstance.ObjectMeta.Generation

	runtimePods := corev1.PodList{}
	filterLabels := client.MatchingLabels{utils.RayClusterLabelKey: newInstance.Name}
	if err := r.List(ctx, &runtimePods, client.InNamespace(newInstance.Namespace), filterLabels); err != nil {
		return nil, err
	}

	newInstance.Status.ReadyWorkerReplicas = utils.CalculateReadyReplicas(runtimePods)
	newInstance.Status.AvailableWorkerReplicas = utils.CalculateAvailableReplicas(runtimePods)
	newInstance.Status.DesiredWorkerReplicas = utils.CalculateDesiredReplicas(ctx, newInstance)
	newInstance.Status.MinWorkerReplicas = utils.CalculateMinReplicas(newInstance)
	newInstance.Status.MaxWorkerReplicas = utils.CalculateMaxReplicas(newInstance)

	totalResources := utils.CalculateDesiredResources(newInstance)
	newInstance.Status.DesiredCPU = totalResources[corev1.ResourceCPU]
	newInstance.Status.DesiredMemory = totalResources[corev1.ResourceMemory]
	newInstance.Status.DesiredGPU = sumGPUs(totalResources)
	newInstance.Status.DesiredTPU = totalResources[corev1.ResourceName("google.com/tpu")]

	if utils.CheckAllPodsRunning(ctx, runtimePods) {
		newInstance.Status.State = rayv1.Ready
	}

	if newInstance.Spec.Suspend != nil && *newInstance.Spec.Suspend && len(runtimePods.Items) == 0 {
		newInstance.Status.State = rayv1.Suspended
	}

	if err := r.updateEndpoints(ctx, newInstance); err != nil {
		return nil, err
	}

	if err := r.updateHeadInfo(ctx, newInstance); err != nil {
		return nil, err
	}

	timeNow := metav1.Now()
	newInstance.Status.LastUpdateTime = &timeNow

	if instance.Status.State != newInstance.Status.State {
		if newInstance.Status.StateTransitionTimes == nil {
			newInstance.Status.StateTransitionTimes = make(map[rayv1.ClusterState]*metav1.Time)
		}
		newInstance.Status.StateTransitionTimes[newInstance.Status.State] = &timeNow
	}

	return newInstance, nil
}

// Best effort to obtain the ip of the head node.
func (r *RayClusterReconciler) getHeadPodIP(ctx context.Context, instance *rayv1.RayCluster) (string, error) {
	logger := ctrl.LoggerFrom(ctx)

	runtimePods := corev1.PodList{}
	filterLabels := client.MatchingLabels{utils.RayClusterLabelKey: instance.Name, utils.RayNodeTypeLabelKey: string(rayv1.HeadNode)}
	if err := r.List(ctx, &runtimePods, client.InNamespace(instance.Namespace), filterLabels); err != nil {
		return "", err
	}
	if len(runtimePods.Items) != 1 {
		logger.Info(fmt.Sprintf("Found %d head pods. cluster name %s, filter labels %v", len(runtimePods.Items), instance.Name, filterLabels))
		return "", nil
	}
	return runtimePods.Items[0].Status.PodIP, nil
}

func (r *RayClusterReconciler) getHeadServiceIPAndName(ctx context.Context, instance *rayv1.RayCluster) (string, string, error) {
	runtimeServices := corev1.ServiceList{}
	if err := r.List(ctx, &runtimeServices, common.RayClusterHeadServiceListOptions(instance)...); err != nil {
		return "", "", err
	}
	if len(runtimeServices.Items) < 1 {
		return "", "", fmt.Errorf("unable to find head service. cluster name %s, filter labels %v", instance.Name, common.RayClusterHeadServiceListOptions(instance))
	} else if len(runtimeServices.Items) > 1 {
		return "", "", fmt.Errorf("found multiple head services. cluster name %s, filter labels %v", instance.Name, common.RayClusterHeadServiceListOptions(instance))
	} else if runtimeServices.Items[0].Spec.ClusterIP == "" {
		return "", "", fmt.Errorf("head service IP is empty. cluster name %s, filter labels %v", instance.Name, common.RayClusterHeadServiceListOptions(instance))
	} else if runtimeServices.Items[0].Spec.ClusterIP == corev1.ClusterIPNone {
		// We return Head Pod IP if the Head service is headless.
		ip, err := r.getHeadPodIP(ctx, instance)
		if err != nil {
			return "", "", err
		}
		return ip, runtimeServices.Items[0].Name, nil
	}

	return runtimeServices.Items[0].Spec.ClusterIP, runtimeServices.Items[0].Name, nil
}

func (r *RayClusterReconciler) updateEndpoints(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	// TODO: (@scarlet25151) There may be several K8s Services for a RayCluster.
	// We assume we can find the right one by filtering Services with appropriate label selectors
	// and picking the first one. We may need to select by name in the future if the Service naming is stable.
	rayHeadSvc := corev1.ServiceList{}
	filterLabels := client.MatchingLabels{
		utils.RayClusterLabelKey:  instance.Name,
		utils.RayNodeTypeLabelKey: "head",
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
				logger.Info("updateStatus", "service port's name is empty. Not adding it to RayCluster status.endpoints", port)
				continue
			}
			if port.NodePort != 0 {
				instance.Status.Endpoints[port.Name] = fmt.Sprintf("%d", port.NodePort)
			} else if port.TargetPort.IntVal != 0 {
				instance.Status.Endpoints[port.Name] = fmt.Sprintf("%d", port.TargetPort.IntVal)
			} else if port.TargetPort.StrVal != "" {
				instance.Status.Endpoints[port.Name] = port.TargetPort.StrVal
			} else {
				logger.Info("updateStatus", "service port's targetPort is empty. Not adding it to RayCluster status.endpoints", port)
			}
		}
	} else {
		logger.Info("updateEndpoints", "unable to find a Service for this RayCluster. Not adding RayCluster status.endpoints", instance.Name, "Service selectors", filterLabels)
	}

	return nil
}

func (r *RayClusterReconciler) updateHeadInfo(ctx context.Context, instance *rayv1.RayCluster) error {
	ip, err := r.getHeadPodIP(ctx, instance)
	if err != nil {
		return err
	}
	instance.Status.Head.PodIP = ip

	ip, name, err := r.getHeadServiceIPAndName(ctx, instance)
	if err != nil {
		return err
	}
	instance.Status.Head.ServiceIP = ip
	instance.Status.Head.ServiceName = name

	return nil
}

func (r *RayClusterReconciler) reconcileAutoscalerServiceAccount(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	serviceAccount := &corev1.ServiceAccount{}
	namespacedName := common.RayClusterAutoscalerServiceAccountNamespacedName(instance)

	if err := r.Get(ctx, namespacedName, serviceAccount); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// If users specify ServiceAccountName for the head Pod, they need to create a ServiceAccount themselves.
		// However, if KubeRay creates a ServiceAccount for users, the autoscaler may encounter permission issues during
		// zero-downtime rolling updates when RayService is performed. See https://github.com/ray-project/kuberay/issues/1123
		// for more details.
		if instance.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName == namespacedName.Name {
			logger.Error(err, fmt.Sprintf(
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
				logger.Info("Pod service account already exist, no need to create")
				return nil
			}
			return err
		}
		logger.Info("Pod ServiceAccount created successfully", "service account name", serviceAccount.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created service account %s", serviceAccount.Name)
		return nil
	}

	return nil
}

func (r *RayClusterReconciler) reconcileAutoscalerRole(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	role := &rbacv1.Role{}
	namespacedName := common.RayClusterAutoscalerRoleNamespacedName(instance)
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
				logger.Info("role already exist, no need to create")
				return nil
			}
			return err
		}
		logger.Info("Role created successfully", "role name", role.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created role %s", role.Name)
		return nil
	}

	return nil
}

func (r *RayClusterReconciler) reconcileAutoscalerRoleBinding(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	if instance.Spec.EnableInTreeAutoscaling == nil || !*instance.Spec.EnableInTreeAutoscaling {
		return nil
	}

	roleBinding := &rbacv1.RoleBinding{}
	namespacedName := common.RayClusterAutoscalerRoleBindingNamespacedName(instance)
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
				logger.Info("role binding already exist, no need to create")
				return nil
			}
			return err
		}
		logger.Info("RoleBinding created successfully", "role binding name", roleBinding.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Created", "Created role binding %s", roleBinding.Name)
		return nil
	}

	return nil
}

func (r *RayClusterReconciler) updateClusterState(ctx context.Context, instance *rayv1.RayCluster, clusterState rayv1.ClusterState) error {
	logger := ctrl.LoggerFrom(ctx)
	if instance.Status.State == clusterState {
		return nil
	}
	instance.Status.State = clusterState
	logger.Info("updateClusterState", "Update CR Status.State", clusterState)
	return r.Status().Update(ctx, instance)
}

func (r *RayClusterReconciler) updateClusterReason(ctx context.Context, instance *rayv1.RayCluster, clusterReason string) error {
	logger := ctrl.LoggerFrom(ctx)
	if instance.Status.Reason == clusterReason {
		return nil
	}
	instance.Status.Reason = clusterReason
	logger.Info("updateClusterReason", "Update CR Status.Reason", clusterReason)
	return r.Status().Update(ctx, instance)
}

// sumGPUs sums the GPUs in the given resource list.
func sumGPUs(resources map[corev1.ResourceName]resource.Quantity) resource.Quantity {
	totalGPUs := resource.Quantity{}

	for key, val := range resources {
		if strings.HasSuffix(string(key), "gpu") && !val.IsZero() {
			totalGPUs.Add(val)
		}
	}

	return totalGPUs
}
