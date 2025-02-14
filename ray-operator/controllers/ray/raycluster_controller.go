package ray

import (
	"context"
	errstd "errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/expectations"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"

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
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type reconcileFunc func(context.Context, *rayv1.RayCluster) error

var (
	DefaultRequeueDuration = 2 * time.Second

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
func NewReconciler(ctx context.Context, mgr manager.Manager, options RayClusterReconcilerOptions, rayConfigs configapi.Configuration) *RayClusterReconciler {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, podUIDIndexField, func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{string(pod.UID)}
	}); err != nil {
		panic(err)
	}
	isOpenShift := getClusterType(ctx)
	// init the batch scheduler manager
	schedulerMgr, err := batchscheduler.NewSchedulerManager(rayConfigs, mgr.GetConfig())
	if err != nil {
		// fail fast if the scheduler plugin fails to init
		// prevent running the controller in an undefined state
		panic(err)
	}

	// add schema to runtime
	schedulerMgr.AddToScheme(mgr.GetScheme())
	return &RayClusterReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorderFor("raycluster-controller"),
		BatchSchedulerMgr: schedulerMgr,
		IsOpenShift:       isOpenShift,

		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(mgr.GetClient()),
		headSidecarContainers:      options.HeadSidecarContainers,
		workerSidecarContainers:    options.WorkerSidecarContainers,
	}
}

// RayClusterReconciler reconciles a RayCluster object
type RayClusterReconciler struct {
	client.Client
	Scheme                     *k8sruntime.Scheme
	Recorder                   record.EventRecorder
	BatchSchedulerMgr          *batchscheduler.SchedulerManager
	rayClusterScaleExpectation expectations.RayClusterScaleExpectation

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
		return r.rayClusterReconcile(ctx, instance)
	}

	// No match found
	if errors.IsNotFound(err) {
		// Clear all related expectations
		r.rayClusterScaleExpectation.Delete(instance.Name, instance.Namespace)
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

func (r *RayClusterReconciler) rayClusterReconcile(ctx context.Context, instance *rayv1.RayCluster) (ctrl.Result, error) {
	var reconcileErr error
	logger := ctrl.LoggerFrom(ctx)

	if manager := utils.ManagedByExternalController(instance.Spec.ManagedBy); manager != nil {
		logger.Info("Skipping RayCluster managed by a custom controller", "managed-by", manager)
		return ctrl.Result{}, nil
	}

	if err := utils.ValidateRayClusterSpec(instance); err != nil {
		logger.Error(err, fmt.Sprintf("The RayCluster spec is invalid %s/%s", instance.Namespace, instance.Name))
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.InvalidRayClusterSpec),
			"The RayCluster spec is invalid %s/%s: %v", instance.Namespace, instance.Name, err)
		return ctrl.Result{}, nil
	}

	if err := utils.ValidateRayClusterStatus(instance); err != nil {
		logger.Error(err, "The RayCluster status is invalid")
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.InvalidRayClusterStatus),
			"The RayCluster status is invalid %s/%s, %v", instance.Namespace, instance.Name, err)
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// Please do NOT modify `originalRayClusterInstance` in the following code.
	originalRayClusterInstance := instance.DeepCopy()

	// The `enableGCSFTRedisCleanup` is a feature flag introduced in KubeRay v1.0.0. It determines whether
	// the Redis cleanup job should be activated. Users can disable the feature by setting the environment
	// variable `ENABLE_GCS_FT_REDIS_CLEANUP` to `false`, and undertake the Redis storage namespace cleanup
	// manually after the RayCluster CR deletion.
	enableGCSFTRedisCleanup := strings.ToLower(os.Getenv(utils.ENABLE_GCS_FT_REDIS_CLEANUP)) != "false"

	if enableGCSFTRedisCleanup && utils.IsGCSFaultToleranceEnabled(*instance) {
		if instance.DeletionTimestamp.IsZero() {
			if !controllerutil.ContainsFinalizer(instance, utils.GCSFaultToleranceRedisCleanupFinalizer) {
				logger.Info(
					"GCS fault tolerance has been enabled. Implementing a finalizer to ensure that Redis is properly cleaned up once the RayCluster custom resource (CR) is deleted.",
					"finalizer", utils.GCSFaultToleranceRedisCleanupFinalizer)
				controllerutil.AddFinalizer(instance, utils.GCSFaultToleranceRedisCleanupFinalizer)
				if err := r.Update(ctx, instance); err != nil {
					err = fmt.Errorf("failed to add the finalizer %s to the RayCluster: %w", utils.GCSFaultToleranceRedisCleanupFinalizer, err)
					return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
				}
				// Only start the RayCluster reconciliation after the finalizer is added.
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, nil
			}
		} else {
			logger.Info(
				"The RayCluster with GCS enabled is being deleted. Start to handle the Redis cleanup finalizer.",
				"redisCleanupFinalizer", utils.GCSFaultToleranceRedisCleanupFinalizer,
				"deletionTimestamp", instance.ObjectMeta.DeletionTimestamp,
			)

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
				logger.Info(
					"Wait for the head Pod to be terminated before initiating the Redis cleanup process. "+"The storage namespace in Redis cannot be fully deleted if the GCS process on the head Pod is still writing to it.",
					"headPodName", headPods.Items[0].Name,
					"redisStorageNamespace", headPods.Items[0].Annotations[utils.RayExternalStorageNSAnnotationKey],
				)
				// Requeue after 10 seconds because it takes much longer than DefaultRequeueDuration (2 seconds) for the head Pod to be terminated.
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}

			filterLabels := common.RayClusterRedisCleanupJobAssociationOptions(instance).ToListOptions()
			redisCleanupJobs := batchv1.JobList{}
			if err := r.List(ctx, &redisCleanupJobs, filterLabels...); err != nil {
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
						logger.Info(
							"The Redis cleanup Job has been completed. "+
								"The storage namespace in Redis has been fully deleted.",
							"redisCleanupJobName", redisCleanupJob.Name,
							"redisStorageNamespace", redisCleanupJob.Annotations[utils.RayExternalStorageNSAnnotationKey],
						)
					case batchv1.JobFailed:
						logger.Info(
							"The Redis cleanup Job has failed, requeue the RayCluster CR after 5 minute. "+
								"You should manually delete the storage namespace in Redis and remove the RayCluster's finalizer. "+
								"Please check https://docs.ray.io/en/master/cluster/kubernetes/user-guides/kuberay-gcs-ft.html for more details.",
							"redisCleanupJobName", redisCleanupJob.Name,
							"redisStorageNamespace", redisCleanupJob.Annotations[utils.RayExternalStorageNSAnnotationKey],
						)
					}
					return ctrl.Result{}, nil
				}
				// the redisCleanupJob is still running
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, nil
			}
			redisCleanupJob := r.buildRedisCleanupJob(ctx, *instance)
			if err := r.Create(ctx, &redisCleanupJob); err != nil {
				if errors.IsAlreadyExists(err) {
					logger.Info("Redis cleanup Job already exists. Requeue the RayCluster CR.")
					return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, nil
				}
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateRedisCleanupJob),
					"Failed to create Redis cleanup Job %s/%s, %v", redisCleanupJob.Namespace, redisCleanupJob.Name, err)
				return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
			}
			logger.Info("Created Redis cleanup Job", "name", redisCleanupJob.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedRedisCleanupJob),
				"Created Redis cleanup Job %s/%s", redisCleanupJob.Namespace, redisCleanupJob.Name)
			return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, nil
		}
	}

	if instance.DeletionTimestamp != nil && !instance.DeletionTimestamp.IsZero() {
		logger.Info("RayCluster is being deleted, just ignore")
		return ctrl.Result{}, nil
	}

	reconcileFuncs := []reconcileFunc{
		r.reconcileAutoscalerServiceAccount,
		r.reconcileAutoscalerRole,
		r.reconcileAutoscalerRoleBinding,
		r.reconcileIngress,
		r.reconcileHeadService,
		r.reconcileHeadlessService,
		r.reconcileServeService,
		r.reconcilePods,
	}

	for _, fn := range reconcileFuncs {
		if reconcileErr = fn(ctx, instance); reconcileErr != nil {
			funcName := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
			logger.Error(reconcileErr, "Error reconcile resources", "function name", funcName)
			break
		}
	}

	// Calculate the new status for the RayCluster. Note that the function will deep copy `instance` instead of mutating it.
	newInstance, calculateErr := r.calculateStatus(ctx, instance, reconcileErr)
	var updateErr error
	var inconsistent bool
	if calculateErr != nil {
		logger.Info("Got error when calculating new status", "error", calculateErr)
	} else {
		inconsistent, updateErr = r.updateRayClusterStatus(ctx, originalRayClusterInstance, newInstance)
	}

	// Return error based on order.
	var err error
	if reconcileErr != nil {
		err = reconcileErr
	} else if calculateErr != nil {
		err = calculateErr
	} else {
		err = updateErr
	}
	// If the custom resource's status is updated, requeue the reconcile key.
	// Without this behavior, atomic operations such as the suspend operation would need to wait for `RAYCLUSTER_DEFAULT_REQUEUE_SECONDS` to delete Pods
	// after the condition rayv1.RayClusterSuspending is set to true.
	if err != nil || inconsistent {
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// Unconditionally requeue after the number of seconds specified in the
	// environment variable RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV. If the
	// environment variable is not set, requeue after the default value.
	requeueAfterSeconds, err := strconv.Atoi(os.Getenv(utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV))
	if err != nil {
		logger.Info(
			"Environment variable is not set, using default value of seconds",
			"environmentVariable", utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV,
			"defaultValue", utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS,
		)
		requeueAfterSeconds = utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS
	}
	logger.Info("Unconditional requeue after", "seconds", requeueAfterSeconds)
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

	if oldStatus.State != newStatus.State || oldStatus.Reason != newStatus.Reason { //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
		logger.Info(
			"inconsistentRayClusterStatus",
			"oldState", oldStatus.State, //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
			"newState", newStatus.State, //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
			"oldReason", oldStatus.Reason,
			"newReason", newStatus.Reason,
		)
		return true
	}
	if oldStatus.ReadyWorkerReplicas != newStatus.ReadyWorkerReplicas ||
		oldStatus.AvailableWorkerReplicas != newStatus.AvailableWorkerReplicas ||
		oldStatus.DesiredWorkerReplicas != newStatus.DesiredWorkerReplicas ||
		oldStatus.MinWorkerReplicas != newStatus.MinWorkerReplicas ||
		oldStatus.MaxWorkerReplicas != newStatus.MaxWorkerReplicas {
		logger.Info(
			"inconsistentRayClusterStatus",
			"oldReadyWorkerReplicas", oldStatus.ReadyWorkerReplicas,
			"newReadyWorkerReplicas", newStatus.ReadyWorkerReplicas,
			"oldAvailableWorkerReplicas", oldStatus.AvailableWorkerReplicas,
			"newAvailableWorkerReplicas", newStatus.AvailableWorkerReplicas,
			"oldDesiredWorkerReplicas", oldStatus.DesiredWorkerReplicas,
			"newDesiredWorkerReplicas", newStatus.DesiredWorkerReplicas,
			"oldMinWorkerReplicas", oldStatus.MinWorkerReplicas,
			"newMinWorkerReplicas", newStatus.MinWorkerReplicas,
			"oldMaxWorkerReplicas", oldStatus.MaxWorkerReplicas,
			"newMaxWorkerReplicas", newStatus.MaxWorkerReplicas,
		)
		return true
	}
	if !reflect.DeepEqual(oldStatus.Endpoints, newStatus.Endpoints) || !reflect.DeepEqual(oldStatus.Head, newStatus.Head) {
		logger.Info(
			"inconsistentRayClusterStatus",
			"oldEndpoints", oldStatus.Endpoints,
			"newEndpoints", newStatus.Endpoints,
			"oldHead", oldStatus.Head,
			"newHead", newStatus.Head,
		)
		return true
	}
	if !reflect.DeepEqual(oldStatus.Conditions, newStatus.Conditions) {
		logger.Info("inconsistentRayClusterStatus", "old conditions", oldStatus.Conditions, "new conditions", newStatus.Conditions)
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
	filterLabels := common.RayClusterNetworkResourcesOptions(instance).ToListOptions()
	if err := r.List(ctx, &headRoutes, filterLabels...); err != nil {
		return err
	}

	if len(headRoutes.Items) == 1 {
		logger.Info("reconcileIngresses", "head service route found", headRoutes.Items[0].Name)
		return nil
	}

	if len(headRoutes.Items) == 0 {
		route, err := common.BuildRouteForHeadService(*instance)
		if err != nil {
			return err
		}

		if err := ctrl.SetControllerReference(instance, route, r.Scheme); err != nil {
			return err
		}

		if err := r.createHeadRoute(ctx, route, instance); err != nil {
			return err
		}
	}

	return nil
}

func (r *RayClusterReconciler) reconcileIngressKubernetes(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	headIngresses := networkingv1.IngressList{}
	filterLabels := common.RayClusterNetworkResourcesOptions(instance).ToListOptions()
	if err := r.List(ctx, &headIngresses, filterLabels...); err != nil {
		return err
	}

	if len(headIngresses.Items) == 1 {
		logger.Info("reconcileIngresses", "head service ingress found", headIngresses.Items[0].Name)
		return nil
	}

	if len(headIngresses.Items) == 0 {
		ingress, err := common.BuildIngressForHeadService(ctx, *instance)
		if err != nil {
			return err
		}

		if err := ctrl.SetControllerReference(instance, ingress, r.Scheme); err != nil {
			return err
		}

		if err := r.createHeadIngress(ctx, ingress, instance); err != nil {
			return err
		}
	}

	return nil
}

// Return nil only when the head service successfully created or already exists.
func (r *RayClusterReconciler) reconcileHeadService(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	services := corev1.ServiceList{}
	filterLabels := common.RayClusterHeadServiceListOptions(instance)

	if err := r.List(ctx, &services, filterLabels...); err != nil {
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
			logger.Info("Ray head service does not have any ports set up.", "serviceSpecification", headSvc.Spec)
			return fmt.Errorf("ray head service does not have any ports set up. Service specification: %v", headSvc.Spec)
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
	// Only reconcile the K8s service for Ray Serve when the "ray.io/enable-serve-service" annotation is set to true.
	if enableServeServiceValue, exist := instance.Annotations[utils.EnableServeServiceKey]; !exist || enableServeServiceValue != utils.EnableServeServiceTrue {
		return nil
	}

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
		headlessSvc := common.BuildHeadlessServiceForRayCluster(*instance)

		if err := r.createService(ctx, headlessSvc, instance); err != nil {
			return err
		}
	}

	return nil
}

func (r *RayClusterReconciler) reconcilePods(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// if RayCluster is suspending, delete all pods and skip reconcile
	suspendStatus := utils.FindRayClusterSuspendStatus(instance)
	statusConditionGateEnabled := features.Enabled(features.RayClusterStatusConditions)
	if suspendStatus == rayv1.RayClusterSuspending ||
		(!statusConditionGateEnabled && instance.Spec.Suspend != nil && *instance.Spec.Suspend) {
		if _, err := r.deleteAllPods(ctx, common.RayClusterAllPodsAssociationOptions(instance)); err != nil {
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToDeletePodCollection),
				"Failed deleting Pods due to suspension for RayCluster %s/%s, %v",
				instance.Namespace, instance.Name, err)
			return errstd.Join(utils.ErrFailedDeleteAllPods, err)
		}

		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedPod),
			"Deleted Pods for RayCluster %s/%s due to suspension",
			instance.Namespace, instance.Name)
		return nil
	}

	if statusConditionGateEnabled {
		if suspendStatus == rayv1.RayClusterSuspended {
			return nil // stop reconcilePods because the cluster is suspended.
		}
		// (suspendStatus != rayv1.RayClusterSuspending) is always true here because it has been checked above.
		if instance.Spec.Suspend != nil && *instance.Spec.Suspend {
			return nil // stop reconcilePods because the cluster is going to suspend.
		}
	}

	// check if all the pods exist
	headPods := corev1.PodList{}
	if err := r.List(ctx, &headPods, common.RayClusterHeadPodsAssociationOptions(instance).ToListOptions()...); err != nil {
		return err
	}
	// check if the batch scheduler integration is enabled
	// call the scheduler plugin if so
	if r.BatchSchedulerMgr != nil {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(); err == nil {
			if err := scheduler.DoBatchSchedulingOnSubmission(ctx, instance); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	// Reconcile head Pod
	if !r.rayClusterScaleExpectation.IsSatisfied(ctx, instance.Namespace, instance.Name, expectations.HeadGroup) {
		logger.Info("reconcilePods", "Expectation", "NotSatisfiedHeadExpectations, reconcile head later")
	} else if len(headPods.Items) == 1 {
		headPod := headPods.Items[0]
		logger.Info("reconcilePods", "Found 1 head Pod", headPod.Name, "Pod status", headPod.Status.Phase,
			"Pod status reason", headPod.Status.Reason,
			"Pod restart policy", headPod.Spec.RestartPolicy,
			"Ray container terminated status", getRayContainerStateTerminated(headPod))

		shouldDelete, reason := shouldDeletePod(headPod, rayv1.HeadNode)
		logger.Info("reconcilePods", "head Pod", headPod.Name, "shouldDelete", shouldDelete, "reason", reason)
		if shouldDelete {
			if err := r.Delete(ctx, &headPod); err != nil {
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToDeleteHeadPod),
					"Failed deleting head Pod %s/%s; Pod status: %s; Pod restart policy: %s; Ray container terminated status: %v, %v",
					headPod.Namespace, headPod.Name, headPod.Status.Phase, headPod.Spec.RestartPolicy, getRayContainerStateTerminated(headPod), err)
				return errstd.Join(utils.ErrFailedDeleteHeadPod, err)
			}
			r.rayClusterScaleExpectation.ExpectScalePod(headPod.Namespace, instance.Name, expectations.HeadGroup, headPod.Name, expectations.Delete)
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedHeadPod),
				"Deleted head Pod %s/%s; Pod status: %s; Pod restart policy: %s; Ray container terminated status: %v",
				headPod.Namespace, headPod.Name, headPod.Status.Phase, headPod.Spec.RestartPolicy, getRayContainerStateTerminated(headPod))
			return errstd.New(reason)
		}
	} else if len(headPods.Items) == 0 {
		// Create head Pod if it does not exist.
		logger.Info("reconcilePods: Found 0 head Pods; creating a head Pod for the RayCluster.")
		common.CreatedClustersCounterInc(instance.Namespace)
		if err := r.createHeadPod(ctx, *instance); err != nil {
			common.FailedClustersCounterInc(instance.Namespace)
			return errstd.Join(utils.ErrFailedCreateHeadPod, err)
		}
		common.SuccessfulClustersCounterInc(instance.Namespace)
	} else if len(headPods.Items) > 1 {
		logger.Info("reconcilePods: Found more than one head Pods; deleting extra head Pods.", "nHeadPods", len(headPods.Items))
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
				return errstd.Join(utils.ErrFailedDeleteHeadPod, err)
			}
			r.rayClusterScaleExpectation.ExpectScalePod(extraHeadPodToDelete.Namespace, instance.Name, expectations.HeadGroup, extraHeadPodToDelete.Name, expectations.Delete)
		}
	}

	// Reconcile worker pods now
	for _, worker := range instance.Spec.WorkerGroupSpecs {
		if !r.rayClusterScaleExpectation.IsSatisfied(ctx, instance.Namespace, instance.Name, worker.GroupName) {
			logger.Info("reconcilePods", "worker group", worker.GroupName, "Expectation", "NotSatisfiedGroupExpectations, reconcile the group later")
			continue
		}
		// workerReplicas will store the target number of pods for this worker group.
		var workerReplicas int32 = utils.GetWorkerGroupDesiredReplicas(ctx, worker)
		logger.Info("reconcilePods", "desired workerReplicas (always adhering to minReplicas/maxReplica)", workerReplicas, "worker group", worker.GroupName, "maxReplicas", worker.MaxReplicas, "minReplicas", worker.MinReplicas, "replicas", worker.Replicas)

		workerPods := corev1.PodList{}
		if err := r.List(ctx, &workerPods, common.RayClusterGroupPodsAssociationOptions(instance, worker.GroupName).ToListOptions()...); err != nil {
			return err
		}

		// Delete all workers if worker group is suspended and skip reconcile
		if worker.Suspend != nil && *worker.Suspend {
			if _, err := r.deleteAllPods(ctx, common.RayClusterGroupPodsAssociationOptions(instance, worker.GroupName)); err != nil {
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToDeleteWorkerPodCollection),
					"Failed deleting worker Pods for suspended group %s in RayCluster %s/%s, %v", worker.GroupName, instance.Namespace, instance.Name, err)
				return errstd.Join(utils.ErrFailedDeleteWorkerPod, err)
			}
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedWorkerPod),
				"Deleted all pods for suspended worker group %s in RayCluster %s/%s", worker.GroupName, instance.Namespace, instance.Name)
			continue
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
					r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToDeleteWorkerPod),
						"Failed deleting worker Pod %s/%s; Pod status: %s; Pod restart policy: %s; Ray container terminated status: %v, %v",
						workerPod.Namespace, workerPod.Name, workerPod.Status.Phase, workerPod.Spec.RestartPolicy, getRayContainerStateTerminated(workerPod), err)
					return errstd.Join(utils.ErrFailedDeleteWorkerPod, err)
				}
				r.rayClusterScaleExpectation.ExpectScalePod(workerPod.Namespace, instance.Name, worker.GroupName, workerPod.Name, expectations.Delete)
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedWorkerPod),
					"Deleted worker Pod %s/%s; Pod status: %s; Pod restart policy: %s; Ray container terminated status: %v",
					workerPod.Namespace, workerPod.Name, workerPod.Status.Phase, workerPod.Spec.RestartPolicy, getRayContainerStateTerminated(workerPod))
			}
		}

		// If we delete unhealthy Pods, we will not create new Pods in this reconciliation.
		if numDeletedUnhealthyWorkerPods > 0 {
			return fmt.Errorf("delete %d unhealthy worker Pods", numDeletedUnhealthyWorkerPods)
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
					r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToDeleteWorkerPod), "Failed deleting pod %s/%s, %v", pod.Namespace, pod.Name, err)
					return errstd.Join(utils.ErrFailedDeleteWorkerPod, err)
				}
				logger.Info("reconcilePods", "The worker Pod has already been deleted", pod.Name)
			} else {
				r.rayClusterScaleExpectation.ExpectScalePod(pod.Namespace, instance.Name, worker.GroupName, pod.Name, expectations.Delete)
				deletedWorkers[pod.Name] = deleted
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedWorkerPod), "Deleted pod %s/%s", pod.Namespace, pod.Name)
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
		numExpectedPods := int(workerReplicas * worker.NumOfHosts)
		diff := numExpectedPods - len(runningPods.Items)

		logger.Info("reconcilePods", "workerReplicas", workerReplicas, "NumOfHosts", worker.NumOfHosts, "runningPods", len(runningPods.Items), "diff", diff)

		if diff > 0 {
			// pods need to be added
			logger.Info("reconcilePods", "Number workers to add", diff, "Worker group", worker.GroupName)
			// create all workers of this group
			for i := 0; i < diff; i++ {
				logger.Info("reconcilePods", "creating worker for group", worker.GroupName, "index", i, "total", diff)
				if err := r.createWorkerPod(ctx, *instance, *worker.DeepCopy()); err != nil {
					return errstd.Join(utils.ErrFailedCreateWorkerPod, err)
				}
			}
		} else if diff == 0 {
			logger.Info("reconcilePods", "all workers already exist for group", worker.GroupName)
			continue
		} else {
			// diff < 0 indicates the need to delete some Pods to match the desired number of replicas. However,
			// randomly deleting Pods is certainly not ideal. So, if autoscaling is enabled for the cluster, we
			// will disable random Pod deletion, making Autoscaler the sole decision-maker for Pod deletions.
			enableInTreeAutoscaling := utils.IsAutoscalingEnabled(instance)

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
				for i := 0; i < randomlyRemovedWorkers; i++ {
					randomPodToDelete := runningPods.Items[i]
					logger.Info("Randomly deleting Pod", "progress", fmt.Sprintf("%d / %d", i+1, randomlyRemovedWorkers), "with name", randomPodToDelete.Name)
					if err := r.Delete(ctx, &randomPodToDelete); err != nil {
						if !errors.IsNotFound(err) {
							r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToDeleteWorkerPod), "Failed deleting Pod %s/%s, %v", randomPodToDelete.Namespace, randomPodToDelete.Name, err)
							return errstd.Join(utils.ErrFailedDeleteWorkerPod, err)
						}
						logger.Info("reconcilePods", "The worker Pod has already been deleted", randomPodToDelete.Name)
					}
					r.rayClusterScaleExpectation.ExpectScalePod(randomPodToDelete.Namespace, instance.Name, worker.GroupName, randomPodToDelete.Name, expectations.Delete)
					r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedWorkerPod), "Deleted Pod %s/%s", randomPodToDelete.Namespace, randomPodToDelete.Name)
				}
			} else {
				logger.Info("Random Pod deletion is disabled for the cluster. The only decision-maker for Pod deletions is Autoscaler.")
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
	// Based on the logic of the change of the status of the K8S pod, the following judgment is made.
	// https://github.com/kubernetes/kubernetes/blob/3361895612dac57044d5dacc029d2ace1865479c/pkg/kubelet/kubelet_pods.go#L1556

	// If the Pod's status is `Failed` or `Succeeded`, the Pod will not restart and we can safely delete it.
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		reason := fmt.Sprintf(
			"The %s Pod %s status is %s which is a terminal state. "+
				"KubeRay will delete the Pod and create new Pods in the next reconciliation if necessary.",
			nodeType, pod.Name, pod.Status.Phase)
		return true, reason
	}

	rayContainerTerminated := getRayContainerStateTerminated(pod)
	if pod.Status.Phase == corev1.PodRunning && rayContainerTerminated != nil {
		if pod.Spec.RestartPolicy == corev1.RestartPolicyNever {
			reason := fmt.Sprintf(
				"The Pod status of the %s Pod %s is %s, and the Ray container terminated status is %v. "+
					"The container is unable to restart due to its restart policy %s, so KubeRay will delete it.",
				nodeType, pod.Name, pod.Status.Phase, rayContainerTerminated, pod.Spec.RestartPolicy)
			return true, reason
		}
		// If restart policy is set to `Always` or `OnFailure`, KubeRay will not delete the Pod.
		reason := fmt.Sprintf(
			"The Pod status of the %s Pod %s is %s, and the Ray container terminated status is %v. "+
				"However, KubeRay will not delete the Pod because its restartPolicy is set to %s and it should be able to restart automatically.",
			nodeType, pod.Name, pod.Status.Phase, rayContainerTerminated, pod.Spec.RestartPolicy)
		return false, reason
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
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateIngress), "Failed creating ingress %s/%s, %v", ingress.Namespace, ingress.Name, err)
		return err
	}
	logger.Info("Created ingress for RayCluster", "name", ingress.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedIngress), "Created ingress %s/%s", ingress.Namespace, ingress.Name)
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
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateRoute), "Failed creating route %s/%s, %v", route.Namespace, route.Name, err)
		return err
	}
	logger.Info("Created route for RayCluster", "name", route.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedRoute), "Created route %s/%s", route.Namespace, route.Name)
	return nil
}

func (r *RayClusterReconciler) createService(ctx context.Context, svc *corev1.Service, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// making sure the name is valid
	svc.Name = utils.CheckName(svc.Name)
	if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, svc); err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateService), "Failed creating service %s/%s, %v", svc.Namespace, svc.Name, err)
		return err
	}
	logger.Info("Created service for RayCluster", "name", svc.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedService), "Created service %s/%s", svc.Namespace, svc.Name)
	return nil
}

func (r *RayClusterReconciler) createHeadPod(ctx context.Context, instance rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// build the pod then create it
	pod := r.buildHeadPod(ctx, instance)
	// check if the batch scheduler integration is enabled
	// call the scheduler plugin if so
	if r.BatchSchedulerMgr != nil {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(); err == nil {
			scheduler.AddMetadataToPod(ctx, &instance, utils.RayNodeHeadGroupLabelValue, &pod)
		} else {
			return err
		}
	}

	if err := r.Create(ctx, &pod); err != nil {
		r.Recorder.Eventf(&instance, corev1.EventTypeWarning, string(utils.FailedToCreateHeadPod), "Failed to create head Pod %s/%s, %v", pod.Namespace, pod.Name, err)
		return err
	}
	r.rayClusterScaleExpectation.ExpectScalePod(pod.Namespace, instance.Name, expectations.HeadGroup, pod.Name, expectations.Create)
	logger.Info("Created head Pod for RayCluster", "name", pod.Name)
	r.Recorder.Eventf(&instance, corev1.EventTypeNormal, string(utils.CreatedHeadPod), "Created head Pod %s/%s", pod.Namespace, pod.Name)
	return nil
}

func (r *RayClusterReconciler) createWorkerPod(ctx context.Context, instance rayv1.RayCluster, worker rayv1.WorkerGroupSpec) error {
	logger := ctrl.LoggerFrom(ctx)

	// build the pod then create it
	pod := r.buildWorkerPod(ctx, instance, worker)
	if r.BatchSchedulerMgr != nil {
		if scheduler, err := r.BatchSchedulerMgr.GetSchedulerForCluster(); err == nil {
			scheduler.AddMetadataToPod(ctx, &instance, worker.GroupName, &pod)
		} else {
			return err
		}
	}

	replica := pod
	if err := r.Create(ctx, &replica); err != nil {
		r.Recorder.Eventf(&instance, corev1.EventTypeWarning, string(utils.FailedToCreateWorkerPod), "Failed to create worker Pod for the cluster %s/%s, %v", instance.Namespace, instance.Name, err)
		return err
	}
	r.rayClusterScaleExpectation.ExpectScalePod(replica.Namespace, instance.Name, worker.GroupName, replica.Name, expectations.Create)
	logger.Info("Created worker Pod for RayCluster", "name", replica.Name)
	r.Recorder.Eventf(&instance, corev1.EventTypeNormal, string(utils.CreatedWorkerPod), "Created worker Pod %s/%s", replica.Namespace, replica.Name)
	return nil
}

// Build head instance pod(s).
func (r *RayClusterReconciler) buildHeadPod(ctx context.Context, instance rayv1.RayCluster) corev1.Pod {
	logger := ctrl.LoggerFrom(ctx)
	podName := utils.PodGenerateName(instance.Name, rayv1.HeadNode)
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, instance, instance.Namespace) // Fully Qualified Domain Name
	// The Ray head port used by workers to connect to the cluster (GCS server port for Ray >= 1.11.0, Redis port for older Ray.)
	headPort := common.GetHeadPort(instance.Spec.HeadGroupSpec.RayStartParams)
	autoscalingEnabled := utils.IsAutoscalingEnabled(&instance)
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
	podName := utils.PodGenerateName(fmt.Sprintf("%s-%s", instance.Name, worker.GroupName), rayv1.WorkerNode)
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, instance, instance.Namespace) // Fully Qualified Domain Name

	// The Ray head port used by workers to connect to the cluster (GCS server port for Ray >= 1.11.0, Redis port for older Ray.)
	headPort := common.GetHeadPort(instance.Spec.HeadGroupSpec.RayStartParams)
	autoscalingEnabled := utils.IsAutoscalingEnabled(&instance)
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

	// Build the head pod
	pod := r.buildHeadPod(ctx, instance)
	pod.Labels[utils.RayNodeTypeLabelKey] = string(rayv1.RedisCleanupNode)

	// Only keep the Ray container in the Redis cleanup Job.
	pod.Spec.Containers = []corev1.Container{pod.Spec.Containers[utils.RayContainerIndex]}
	pod.Spec.Containers[utils.RayContainerIndex].Command = []string{"/bin/bash", "-lc", "--"}
	pod.Spec.Containers[utils.RayContainerIndex].Args = []string{
		"echo \"To get more information about manually deleting the storage namespace in Redis and removing the RayCluster's finalizer, please check https://docs.ray.io/en/master/cluster/kubernetes/user-guides/kuberay-gcs-ft.html for more details.\" && " +
			"python -c " +
			"\"from ray._private.gcs_utils import cleanup_redis_storage; " +
			"from urllib.parse import urlparse; " +
			"import os; " +
			"import sys; " +
			"redis_address = os.getenv('RAY_REDIS_ADDRESS', '').split(',')[0]; " +
			"redis_address = redis_address if '://' in redis_address else 'redis://' + redis_address; " +
			"parsed = urlparse(redis_address); ",
	}
	if utils.EnvVarExists(utils.REDIS_USERNAME, pod.Spec.Containers[utils.RayContainerIndex].Env) {
		pod.Spec.Containers[utils.RayContainerIndex].Args[0] += "sys.exit(1) if not cleanup_redis_storage(host=parsed.hostname, port=parsed.port, username=os.getenv('REDIS_USERNAME', parsed.username), password=os.getenv('REDIS_PASSWORD', parsed.password or ''), use_ssl=parsed.scheme=='rediss', storage_namespace=os.getenv('RAY_external_storage_namespace')) else None\""
	} else {
		pod.Spec.Containers[utils.RayContainerIndex].Args[0] += "sys.exit(1) if not cleanup_redis_storage(host=parsed.hostname, port=parsed.port, password=os.getenv('REDIS_PASSWORD', parsed.password or ''), use_ssl=parsed.scheme=='rediss', storage_namespace=os.getenv('RAY_external_storage_namespace')) else None\""
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

	// The container's resource consumption remains constant. Hard-coding the resources is acceptable.
	// Avoid using the GPU for the Redis cleanup Job.
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

	// Trim the job name to ensure it is within the 63-character limit.
	jobName := utils.TrimJobName(fmt.Sprintf("%s-%s", instance.Name, "redis-cleanup"))

	redisCleanupJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName,
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
			// Make this job best-effort only for 5 minutes.
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

	if r.BatchSchedulerMgr != nil {
		r.BatchSchedulerMgr.ConfigureReconciler(b)
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

func (r *RayClusterReconciler) calculateStatus(ctx context.Context, instance *rayv1.RayCluster, reconcileErr error) (*rayv1.RayCluster, error) {
	// TODO: Replace this log and use reconcileErr to set the condition field.
	logger := ctrl.LoggerFrom(ctx)
	if reconcileErr != nil {
		logger.Info("Reconciliation error", "error", reconcileErr)
	}

	// Deep copy the instance, so we don't mutate the original object.
	newInstance := instance.DeepCopy()

	statusConditionGateEnabled := features.Enabled(features.RayClusterStatusConditions)
	if statusConditionGateEnabled {
		if reconcileErr != nil {
			if reason := utils.RayClusterReplicaFailureReason(reconcileErr); reason != "" {
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:    string(rayv1.RayClusterReplicaFailure),
					Status:  metav1.ConditionTrue,
					Reason:  reason,
					Message: reconcileErr.Error(),
				})
			}
		} else {
			// if reconcileErr == nil, we can safely remove the RayClusterReplicaFailure condition.
			meta.RemoveStatusCondition(&newInstance.Status.Conditions, string(rayv1.RayClusterReplicaFailure))
		}
	}

	// TODO (kevin85421): ObservedGeneration should be used to determine whether to update this CR or not.
	newInstance.Status.ObservedGeneration = newInstance.ObjectMeta.Generation

	runtimePods := corev1.PodList{}
	if err := r.List(ctx, &runtimePods, common.RayClusterAllPodsAssociationOptions(newInstance).ToListOptions()...); err != nil {
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

	if reconcileErr == nil && len(runtimePods.Items) == int(newInstance.Status.DesiredWorkerReplicas)+1 { // workers + 1 head
		if utils.CheckAllPodsRunning(ctx, runtimePods) {
			newInstance.Status.State = rayv1.Ready //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
			newInstance.Status.Reason = ""
		}
	}

	// Check if the head node is running and ready by checking the head pod's status or if the cluster has been suspended.
	if statusConditionGateEnabled {
		headPod, err := common.GetRayClusterHeadPod(ctx, r, newInstance)
		if err != nil {
			return nil, err
		}
		// GetRayClusterHeadPod can return nil, nil when pod is not found, we handle it separately.
		if headPod == nil {
			meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
				Type:    string(rayv1.HeadPodReady),
				Status:  metav1.ConditionFalse,
				Reason:  rayv1.HeadPodNotFound,
				Message: "Head Pod not found",
			})
		} else {
			headPodReadyCondition := utils.FindHeadPodReadyCondition(headPod)
			meta.SetStatusCondition(&newInstance.Status.Conditions, headPodReadyCondition)
		}

		suspendStatus := utils.FindRayClusterSuspendStatus(newInstance)
		if !meta.IsStatusConditionTrue(newInstance.Status.Conditions, string(rayv1.RayClusterProvisioned)) && suspendStatus != rayv1.RayClusterSuspended {
			// RayClusterProvisioned indicates whether all Ray Pods are ready when the RayCluster is first created.
			// Note RayClusterProvisioned StatusCondition will not be updated after all Ray Pods are ready for the first time. Unless the cluster has been suspended.
			if utils.CheckAllPodsRunning(ctx, runtimePods) {
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:    string(rayv1.RayClusterProvisioned),
					Status:  metav1.ConditionTrue,
					Reason:  rayv1.AllPodRunningAndReadyFirstTime,
					Message: "All Ray Pods are ready for the first time",
				})
			} else {
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:    string(rayv1.RayClusterProvisioned),
					Status:  metav1.ConditionFalse,
					Reason:  rayv1.RayClusterPodsProvisioning,
					Message: "RayCluster Pods are being provisioned for first time",
				})
			}
		}

		if suspendStatus == rayv1.RayClusterSuspending {
			if len(runtimePods.Items) == 0 {
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:    string(rayv1.RayClusterProvisioned),
					Status:  metav1.ConditionFalse,
					Reason:  rayv1.RayClusterPodsProvisioning,
					Message: "RayCluster has been suspended",
				})
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:   string(rayv1.RayClusterSuspending),
					Reason: string(rayv1.RayClusterSuspending),
					Status: metav1.ConditionFalse,
				})
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:   string(rayv1.RayClusterSuspended),
					Reason: string(rayv1.RayClusterSuspended),
					Status: metav1.ConditionTrue,
				})
			}
		} else if suspendStatus == rayv1.RayClusterSuspended {
			if instance.Spec.Suspend != nil && !*instance.Spec.Suspend {
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:   string(rayv1.RayClusterSuspended),
					Reason: string(rayv1.RayClusterSuspended),
					Status: metav1.ConditionFalse,
				})
			}
		} else {
			meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
				Type:   string(rayv1.RayClusterSuspended),
				Reason: string(rayv1.RayClusterSuspended),
				Status: metav1.ConditionFalse,
			})
			if instance.Spec.Suspend != nil && *instance.Spec.Suspend {
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:   string(rayv1.RayClusterSuspending),
					Reason: string(rayv1.RayClusterSuspending),
					Status: metav1.ConditionTrue,
				})
			} else {
				meta.SetStatusCondition(&newInstance.Status.Conditions, metav1.Condition{
					Type:   string(rayv1.RayClusterSuspending),
					Reason: string(rayv1.RayClusterSuspending),
					Status: metav1.ConditionFalse,
				})
			}
		}
	}

	if newInstance.Spec.Suspend != nil && *newInstance.Spec.Suspend && len(runtimePods.Items) == 0 {
		newInstance.Status.State = rayv1.Suspended //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	}

	if err := r.updateEndpoints(ctx, newInstance); err != nil {
		return nil, err
	}

	if err := r.updateHeadInfo(ctx, newInstance); err != nil {
		return nil, err
	}

	timeNow := metav1.Now()
	newInstance.Status.LastUpdateTime = &timeNow

	if instance.Status.State != newInstance.Status.State { //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
		if newInstance.Status.StateTransitionTimes == nil {
			newInstance.Status.StateTransitionTimes = make(map[rayv1.ClusterState]*metav1.Time)
		}
		newInstance.Status.StateTransitionTimes[newInstance.Status.State] = &timeNow //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	}

	return newInstance, nil
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
		headPod, err := common.GetRayClusterHeadPod(ctx, r, instance)
		if err != nil {
			return "", "", err
		}
		if headPod != nil {
			return headPod.Status.PodIP, runtimeServices.Items[0].Name, nil
		}
		return "", runtimeServices.Items[0].Name, nil
	}

	return runtimeServices.Items[0].Spec.ClusterIP, runtimeServices.Items[0].Name, nil
}

func (r *RayClusterReconciler) updateEndpoints(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	// TODO: (@scarlet25151) There may be several K8s Services for a RayCluster.
	// We assume we can find the right one by filtering Services with appropriate label selectors
	// and picking the first one. We may need to select by name in the future if the Service naming is stable.
	rayHeadSvc := corev1.ServiceList{}
	filterLabels := common.RayClusterHeadServiceListOptions(instance)
	if err := r.List(ctx, &rayHeadSvc, filterLabels...); err != nil {
		return err
	}

	if len(rayHeadSvc.Items) != 0 {
		svc := rayHeadSvc.Items[0]
		if instance.Status.Endpoints == nil {
			instance.Status.Endpoints = map[string]string{}
		}
		for _, port := range svc.Spec.Ports {
			if len(port.Name) == 0 {
				logger.Info("updateStatus: Service port's name is empty. Not adding it to RayCluster status.endpoints", "port", port)
				continue
			}
			if port.NodePort != 0 {
				instance.Status.Endpoints[port.Name] = fmt.Sprintf("%d", port.NodePort)
			} else if port.TargetPort.IntVal != 0 {
				instance.Status.Endpoints[port.Name] = fmt.Sprintf("%d", port.TargetPort.IntVal)
			} else if port.TargetPort.StrVal != "" {
				instance.Status.Endpoints[port.Name] = port.TargetPort.StrVal
			} else {
				logger.Info("updateStatus: Service port's targetPort is empty. Not adding it to RayCluster status.endpoints", "port", port)
			}
		}
	} else {
		logger.Info("updateEndpoints: Unable to find a Service for this RayCluster. Not adding RayCluster status.endpoints", "serviceSelectors", filterLabels)
	}

	return nil
}

func (r *RayClusterReconciler) updateHeadInfo(ctx context.Context, instance *rayv1.RayCluster) error {
	headPod, err := common.GetRayClusterHeadPod(ctx, r, instance)
	if err != nil {
		return err
	}
	if headPod != nil {
		instance.Status.Head.PodIP = headPod.Status.PodIP
		instance.Status.Head.PodName = headPod.Name
	} else {
		instance.Status.Head.PodIP = ""
		instance.Status.Head.PodName = ""
	}

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
	if !utils.IsAutoscalingEnabled(instance) {
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
			actionableMessage := fmt.Sprintf("If users specify ServiceAccountName for the head Pod, they need to create a ServiceAccount themselves. "+
				"However, ServiceAccount %s is not found. Please create one. See the PR description of https://github.com/ray-project/kuberay/pull/1128 for more details.", namespacedName.Name)

			logger.Error(err, actionableMessage)
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.AutoscalerServiceAccountNotFound), "Failed to reconcile RayCluster %s/%s. %s", instance.Namespace, instance.Name, actionableMessage)
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
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateServiceAccount), "Failed creating service account %s/%s, %v", serviceAccount.Namespace, serviceAccount.Name, err)
			return err
		}
		logger.Info("Created service account for Ray Autoscaler", "name", serviceAccount.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedServiceAccount), "Created service account %s/%s", serviceAccount.Namespace, serviceAccount.Name)
		return nil
	}

	return nil
}

func (r *RayClusterReconciler) reconcileAutoscalerRole(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	if !utils.IsAutoscalingEnabled(instance) {
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
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateRole), "Failed creating role %s/%s, %v", role.Namespace, role.Name, err)
			return err
		}
		logger.Info("Created role for Ray Autoscaler", "name", role.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedRole), "Created role %s/%s", role.Namespace, role.Name)
		return nil
	}

	return nil
}

func (r *RayClusterReconciler) reconcileAutoscalerRoleBinding(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)
	if !utils.IsAutoscalingEnabled(instance) {
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
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateRoleBinding), "Failed creating role binding %s/%s, %v", roleBinding.Namespace, roleBinding.Name, err)
			return err
		}
		logger.Info("Created role binding for Ray Autoscaler", "name", roleBinding.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedRoleBinding), "Created role binding %s/%s", roleBinding.Namespace, roleBinding.Name)
		return nil
	}

	return nil
}

// updateRayClusterStatus updates the RayCluster status if it is inconsistent with the old status and returns a bool to indicate the inconsistency.
// We rely on the returning bool to requeue the reconciliation for atomic operations, such as suspending a RayCluster.
func (r *RayClusterReconciler) updateRayClusterStatus(ctx context.Context, originalRayClusterInstance, newInstance *rayv1.RayCluster) (bool, error) {
	logger := ctrl.LoggerFrom(ctx)
	inconsistent := r.inconsistentRayClusterStatus(ctx, originalRayClusterInstance.Status, newInstance.Status)
	if !inconsistent {
		return inconsistent, nil
	}
	logger.Info("updateRayClusterStatus", "name", originalRayClusterInstance.Name, "old status", originalRayClusterInstance.Status, "new status", newInstance.Status)
	err := r.Status().Update(ctx, newInstance)
	if err != nil {
		logger.Info("Error updating status", "name", originalRayClusterInstance.Name, "error", err, "RayCluster", newInstance)
	}
	return inconsistent, err
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
