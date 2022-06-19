package ray

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

const (
	DefaultContainerPort     = 8888
	DefaultContainerPortName = "jupyter"
	BokehContainerPort       = 8889
	BokehContainerPortName   = "bokeh"
	DefaultServingPort       = 80
	PrefixEnvVar             = "NB_PREFIX"
	// TODO(jiaxin@): This should be configurable based on different environment settings.
	DefaultWorkingDir = "/home/jovyan"
	DefaultFSGroup    = int64(100)
	// annotation which indicates stopped status of the workspace
	STOP_ANNOTATION = "workspace.ray.io/stopped"
)

// WorkspaceReconciler reconciles a Workspace object
type WorkspaceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
}

// NewWorkspaceReconciler returns a new reconcile.Reconciler
func NewWorkspaceReconciler(mgr manager.Manager) *WorkspaceReconciler {
	return &WorkspaceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("Workspace"),
		Recorder: mgr.GetEventRecorderFor("workspace-controller"),
	}
}

//+kubebuilder:rbac:groups=ray.io,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ray.io,resources=workspaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ray.io,resources=workspaces/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *WorkspaceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("workspace", request.NamespacedName)
	r.Log.Info("reconciling workspace", "workspace name", request.Name)

	// Fetch the workspace instance.
	workspace := &rayv1alpha1.Workspace{}
	if err := r.Get(ctx, request.NamespacedName, workspace); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			r.Log.Info("Read request instance not found error!")
		} else {
			r.Log.Error(err, "Read request instance error!")
		}
		// Error reading the object - requeue the request
		return ctrl.Result{}, utils.IgnoreNotFound(err)
	}

	if workspace.DeletionTimestamp != nil && !workspace.DeletionTimestamp.IsZero() {
		r.Log.Info("Workspace is being deleted, just ignore", "workspace name", request.Name)
		return ctrl.Result{}, nil
	}

	if err := r.reconcileStatefulSet(workspace); err != nil {
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	if err := r.reconcileService(workspace); err != nil {
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	if err := r.reconcilePod(workspace); err != nil {
		return ctrl.Result{RequeueAfter: DefaultRequeueDuration}, err
	}

	// update the status if needed
	if err := r.updateStatus(workspace); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Update status not found error", "cluster name", request.Name)
		} else {
			r.Log.Error(err, "Update status error", "cluster name", request.Name)
		}
	}
	return ctrl.Result{}, nil
}

// reconcileStatefulSet reconcile the StatefulSet associated with the workspace
func (r *WorkspaceReconciler) reconcileStatefulSet(workspace *rayv1alpha1.Workspace) error {
	// Check if the StatefulSet already exists
	stateful := &appsv1.StatefulSet{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: workspace.Name, Namespace: workspace.Namespace}, stateful); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating StatefulSet", "namespace", workspace.Namespace, "name", workspace.Name)
			// Create a new StatefulSet if not found
			ss := buildStatefulSetForWorkspace(workspace)
			if err := ctrl.SetControllerReference(workspace, ss, r.Scheme); err != nil {
				return err
			}
			if err := r.Create(context.TODO(), ss); err != nil {
				// if the StatefulSet cannot be created we return the error and requeue
				r.Log.Error(err, "Unable to create StatefulSet")
				return err
			}
		} else {
			r.Log.Error(err, "error getting StatefulSet")
			return err
		}
	}

	return nil
}

// reconcileService reconcile the Service belong to the workspace
func (r *WorkspaceReconciler) reconcileService(workspace *rayv1alpha1.Workspace) error {
	// Check if the Service already exists
	svcName := "workspace-svc-" + workspace.Name
	foundService := &corev1.Service{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: workspace.Namespace}, foundService); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating Service", "namespace", workspace.Namespace, "name", workspace.Name)
			// Create a new service if not found
			service := buildServiceForWorkspace(workspace)
			if err := ctrl.SetControllerReference(workspace, service, r.Scheme); err != nil {
				return err
			}
			err = r.Create(context.TODO(), service)
			if err != nil {
				r.Log.Error(err, "unable to create Service")
				return err
			}
		} else {
			r.Log.Error(err, "error getting Service")
			return err
		}
	}

	return nil
}

// reconcilePod reconcile the Pod belong to the statefulset
func (r *WorkspaceReconciler) reconcilePod(workspace *rayv1alpha1.Workspace) error {
	// Generate podName from StatefulSet name.
	podName := fmt.Sprintf("%s-%s", workspace.Name, "0")
	pod := &corev1.Pod{}
	ctx := context.TODO()
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: workspace.Namespace}, pod); err != nil {
		if errors.IsNotFound(err) {
			// It's highly possible pod is not there after StatefulSet is just created. We should reply on the pod events
			r.Log.Info("Workspace pod resource not found. Ignoring since object is not ready or be deleted.")
			return nil
		} else {
			r.Log.Error(err, "error getting Pod")
			return err
		}

	} else {
		// Successfully get the Pod.
		if len(pod.Status.ContainerStatuses) > 0 &&
			pod.Status.ContainerStatuses[0].State != workspace.Status.ContainerState {
			r.Log.Info("Updating container state: ", "state", pod.Status.ContainerStatuses[0].State,
				"namespace", workspace.Namespace, "name", workspace.Name)
			cs := pod.Status.ContainerStatuses[0].State
			workspace.Status.ContainerState = cs
			oldConditions := workspace.Status.Conditions
			newCondition := getWorkspaceCondition(cs)
			// Append new condition
			if len(oldConditions) == 0 || oldConditions[0].Type != newCondition.Type ||
				oldConditions[0].Reason != newCondition.Reason ||
				oldConditions[0].Message != newCondition.Message {
				r.Log.Info("Appending to conditions: ", "namespace", workspace.Namespace, "name", workspace.Name, "type", newCondition.Type, "reason", newCondition.Reason, "message", newCondition.Message)
				workspace.Status.Conditions = append([]rayv1alpha1.WorkspaceCondition{newCondition}, oldConditions...)
			}
			if err := r.Status().Update(ctx, workspace); err != nil {
				return err
			}
		}
	}

	return nil
}

// updateStatus updates the workspace ReadyReplicas status.
func (r *WorkspaceReconciler) updateStatus(workspace *rayv1alpha1.Workspace) error {
	stateful := &appsv1.StatefulSet{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: workspace.Name, Namespace: workspace.Namespace}, stateful); err != nil {
		if errors.IsNotFound(err) {
			return nil
		} else {
			r.Log.Error(err, "error getting StatefulSet")
			return err
		}
	}

	// Update the readyReplicas if the status is changed
	if stateful.Status.ReadyReplicas != workspace.Status.ReadyReplicas {
		r.Log.Info("Updating Status", "namespace", workspace.Namespace, "name", workspace.Name)
		workspace.Status.ReadyReplicas = stateful.Status.ReadyReplicas
		if err := r.Status().Update(context.TODO(), workspace); err != nil {
			return err
		}
	}

	if !IsWorkspaceStopped(workspace) {
		r.Log.Info("pause annotation might be removed on workspace, restart workspace")
		if err := r.updateStatefulSetReplica(context.TODO(), stateful, 1); err != nil {
			return err
		}
	}

	// pause the StatefulSet
	if IsWorkspaceStopped(workspace) {
		r.Log.Info("Find stop annotation on workspace, pausing workspace")
		if err := r.updateStatefulSetReplica(context.TODO(), stateful, 0); err != nil {
			return err
		}
	}

	return nil
}

// updateStatefulSetReplica update replicas and it's mainly used to start/pause the workspace
func (r *WorkspaceReconciler) updateStatefulSetReplica(ctx context.Context, ss *appsv1.StatefulSet, replica int32) error {
	if *ss.Spec.Replicas == replica {
		r.Log.Info(fmt.Sprintf("StatefulSet replica %d, desired replica %d. Skip updating.", *ss.Spec.Replicas, replica))
		return nil
	}

	// set replica and update the StatefulSet
	ss.Spec.Replicas = &replica
	if err := r.Update(ctx, ss); err != nil {
		if errors.IsConflict(err) {
			return err
		}

		r.Log.Error(err, fmt.Sprintf("Unable to update StatefulSet replicas to %d", replica))
		return err
	}

	return nil
}

func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&rayv1alpha1.Workspace{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{})

	controller, err := builder.Build(r)
	if err != nil {
		return err
	}

	// watch underlying pod
	podToRequestsFunc := handler.MapFunc(
		func(a client.Object) []reconcile.Request {
			return []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      a.GetLabels()[common.WorkspaceNameLabelKey],
						Namespace: a.GetNamespace(),
					},
				},
			}
		})

	podPredicates := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if _, ok := e.ObjectOld.GetLabels()[common.WorkspaceNameLabelKey]; !ok {
				return false
			}
			return e.ObjectOld != e.ObjectNew
		},
		CreateFunc: func(e event.CreateEvent) bool {
			if _, ok := e.Object.GetLabels()[common.WorkspaceNameLabelKey]; !ok {
				return false
			}
			return true
		},
	}

	// watch corresponding events
	eventToRequestFunc := handler.MapFunc(
		func(a client.Object) []reconcile.Request {
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      a.GetName(),
						Namespace: a.GetNamespace(),
					},
				},
			}
		})

	eventPredicates := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			event := e.ObjectNew.(*corev1.Event)
			nbName, err := workspaceNameFromInvolvedObject(r.Client, &event.InvolvedObject)
			if err != nil {
				return false
			}
			return e.ObjectOld != e.ObjectNew &&
				isStsOrPodEvent(event) && workspaceExist(r.Client, nbName, e.ObjectNew.GetNamespace())
		},
		CreateFunc: func(e event.CreateEvent) bool {
			event := e.Object.(*corev1.Event)
			nbName, err := workspaceNameFromInvolvedObject(r.Client, &event.InvolvedObject)
			if err != nil {
				return false
			}
			return isStsOrPodEvent(event) && workspaceExist(r.Client, nbName, e.Object.GetNamespace())
		},
	}

	if err = controller.Watch(
		&source.Kind{
			Type: &corev1.Pod{},
		},
		handler.EnqueueRequestsFromMapFunc(podToRequestsFunc),
		podPredicates); err != nil {
		return err
	}

	if err = controller.Watch(
		&source.Kind{
			Type: &corev1.Event{},
		},
		handler.EnqueueRequestsFromMapFunc(eventToRequestFunc),
		eventPredicates); err != nil {
		return err
	}

	return nil
}

func getWorkspaceCondition(cs corev1.ContainerState) rayv1alpha1.WorkspaceCondition {
	workspaceType := ""
	reason := ""
	message := ""

	if cs.Running != nil {
		workspaceType = "Running"
	} else if cs.Waiting != nil {
		workspaceType = "Waiting"
		reason = cs.Waiting.Reason
		message = cs.Waiting.Message
	} else {
		workspaceType = "Terminated"
		reason = cs.Terminated.Reason
		message = cs.Terminated.Reason
	}

	newCondition := rayv1alpha1.WorkspaceCondition{
		Type:          workspaceType,
		LastProbeTime: metav1.Now(),
		Reason:        reason,
		Message:       message,
	}
	return newCondition
}

func buildStatefulSetForWorkspace(instance *rayv1alpha1.Workspace) *appsv1.StatefulSet {
	// Merge workspace labels and annotations to statefulsets.
	labels := map[string]string{}
	for k, v := range instance.GetLabels() {
		labels[k] = v
	}

	annotations := map[string]string{}
	for k, v := range instance.GetAnnotations() {
		annotations[k] = v
	}

	// Set desired replicas based on workspace status.
	replicas := int32(1)
	if _, ok := annotations[STOP_ANNOTATION]; ok {
		replicas = int32(0)
	}

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        instance.Name,
			Namespace:   instance.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					common.WorkspaceNameLabelKey: instance.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					common.WorkspaceStatefulSetNameLabelKey: instance.Name,
					common.WorkspaceNameLabelKey:            instance.Name,
				}},
				Spec: instance.Spec.Template.Spec,
			},
		},
	}

	// copy all of the Workspace labels and annotations to the pod
	if len(ss.Spec.Template.ObjectMeta.Labels) != 0 {
		l := &ss.Spec.Template.ObjectMeta.Labels
		for k, v := range instance.ObjectMeta.Labels {
			(*l)[k] = v
		}
	}

	if len(ss.Spec.Template.ObjectMeta.Annotations) != 0 {
		a := &ss.Spec.Template.ObjectMeta.Annotations
		for k, v := range instance.ObjectMeta.Annotations {
			(*a)[k] = v
		}
	}

	podSpec := &ss.Spec.Template.Spec
	container := &podSpec.Containers[0]
	if container.WorkingDir == "" {
		container.WorkingDir = DefaultWorkingDir
	}
	if container.Ports == nil {
		container.Ports = []corev1.ContainerPort{
			{
				ContainerPort: DefaultContainerPort,
				Name:          DefaultContainerPortName,
				Protocol:      "TCP",
			},
			{
				ContainerPort: BokehContainerPort,
				Name:          BokehContainerPortName,
				Protocol:      "TCP",
			},
		}
	}

	// By default, we like workspace NB_PREFIX to match ingress path, like `/notebook/${namespace}/${name}`
	// Current external endpoint http://${name}-${statefulset}-${namespace}.${cluster}.domain.io
	// Since it already has namespace and name in hostname, we can simplify NB_PREFIX to /kuberay/workspace directly.
	var nbPrefixFound = false
	for _, env := range container.Env {
		// Accept client's request if NB_PREFIX exist
		if env.Name == "NB_PREFIX" {
			nbPrefixFound = true
			break
		}
	}

	if !nbPrefixFound {
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "NB_PREFIX",
			// The major reason to still have `kuberay` is because this path has been
			// configured to be allowed to use websocket which is relied by kernel and terminal from browser to backend
			Value: "/kuberay/workspace",
		})
	}

	// TODO(jiaxin.shan@): Check with k8s team if there's suggested PSP setting
	// This allows for platforms with the limitation to bypass the automatic addition of the fsGroup
	// and will allow for the Pod Security Policy controller to make an appropriate choice
	if value, exists := os.LookupEnv("ADD_FSGROUP"); !exists || value == "true" {
		if podSpec.SecurityContext == nil {
			fsGroup := DefaultFSGroup
			podSpec.SecurityContext = &corev1.PodSecurityContext{
				FSGroup: &fsGroup,
			}
		}
	}

	return ss
}

func buildServiceForWorkspace(instance *rayv1alpha1.Workspace) *corev1.Service {
	// Define the desired Service object
	port := DefaultContainerPort
	containerPorts := instance.Spec.Template.Spec.Containers[0].Ports
	if containerPorts != nil {
		port = int(containerPorts[0].ContainerPort)
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workspace-svc-" + instance.Name,
			Namespace: instance.Namespace,
		},
		Spec: corev1.ServiceSpec{
			// TODO: Make this configurable? In order to make it work in testing env, we choose NodePort to avoid additional port-forwarding.
			Type: "NodePort",
			Selector: map[string]string{
				common.WorkspaceNameLabelKey: instance.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http-" + instance.Name,
					Port:       DefaultServingPort,
					TargetPort: intstr.FromInt(port),
					Protocol:   "TCP",
				},
			},
		},
	}
	return svc
}

func isStsOrPodEvent(event *corev1.Event) bool {
	return event.InvolvedObject.Kind == "Pod" || event.InvolvedObject.Kind == "StatefulSet"
}

// workspaceNameFromInvolvedObject finds workspace CR name from objects
// Examples:
// kubectl get events
// 30m         Normal    Created            pod/my-notebook-0         Created container notebook
// 30m         Normal    Started            pod/my-notebook-0         Started container notebook
// 49m         Normal    SuccessfulCreate   statefulset/my-notebook   create Pod my-notebook-0 in StatefulSet my-notebook successful
// 30m         Normal    SuccessfulCreate   statefulset/my-notebook   create Pod my-notebook-0 in StatefulSet my-notebook successful
func workspaceNameFromInvolvedObject(c client.Client, object *corev1.ObjectReference) (string, error) {
	name, namespace := object.Name, object.Namespace

	if object.Kind == "StatefulSet" {
		return name, nil
	}
	if object.Kind == "Pod" {
		pod := &corev1.Pod{}
		err := c.Get(
			context.TODO(),
			types.NamespacedName{
				Namespace: namespace,
				Name:      name,
			},
			pod,
		)
		if err != nil {
			return "", err
		}
		if nbName, ok := pod.Labels[common.WorkspaceNameLabelKey]; ok {
			return nbName, nil
		}
	}
	return "", fmt.Errorf("object isn't related to a Workspace")
}

func workspaceExist(client client.Client, workspaceName string, namespace string) bool {
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: workspaceName},
		&rayv1alpha1.Workspace{}); err != nil {
		// If error != NotFound, trigger the reconcile call anyway to avoid loosing a potential relevant event
		return !errors.IsNotFound(err)
	}
	return true
}

// IsWorkspaceStopped detects whether the workspace has the pause annotation.
func IsWorkspaceStopped(workspace *rayv1alpha1.Workspace) bool {
	meta := workspace.ObjectMeta
	if meta.GetAnnotations() == nil {
		return false
	}

	if _, ok := meta.GetAnnotations()[STOP_ANNOTATION]; ok {
		return true
	}

	return false
}
