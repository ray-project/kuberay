package volcano

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/go-logr/logr"
	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	volcanoclient "volcano.sh/apis/pkg/client/clientset/versioned"

	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	PodGroupName      = "podgroups.scheduling.volcano.sh"
	QueueNameLabelKey = "volcano.sh/queue-name"
)

type VolcanoBatchScheduler struct {
	extensionClient apiextensionsclient.Interface
	volcanoClient   volcanoclient.Interface
	log             logr.Logger
}

type VolcanoBatchSchedulerFactory struct{}

func GetPluginName() string {
	return "volcano"
}

func (v *VolcanoBatchScheduler) Name() string {
	return GetPluginName()
}

func (v *VolcanoBatchScheduler) DoBatchSchedulingOnSubmission(app *rayv1alpha1.RayCluster) error {
	var minMember int32
	var totalResource corev1.ResourceList
	if app.Spec.EnableInTreeAutoscaling == nil || !*app.Spec.EnableInTreeAutoscaling {
		minMember = utils.CalculateDesiredReplicas(app) + *app.Spec.HeadGroupSpec.Replicas
		totalResource = utils.CalculateDesiredResources(app)
	} else {
		minMember = utils.CalculateMinReplicas(app) + *app.Spec.HeadGroupSpec.Replicas
		totalResource = utils.CalculateMinResources(app)
	}

	if err := v.syncPodGroup(app, minMember, totalResource); err != nil {
		return err
	}
	return nil
}

func (v *VolcanoBatchScheduler) getAppPodGroupName(app *rayv1alpha1.RayCluster) string {
	return fmt.Sprintf("ray-%s-pg", app.Name)
}

func (v *VolcanoBatchScheduler) syncPodGroup(app *rayv1alpha1.RayCluster, size int32, totalResource corev1.ResourceList) error {
	podGroupName := v.getAppPodGroupName(app)
	if pg, err := v.volcanoClient.SchedulingV1beta1().PodGroups(app.Namespace).Get(context.TODO(), podGroupName, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		podGroup := createPodGroup(app, podGroupName, size, totalResource)
		if _, err := v.volcanoClient.SchedulingV1beta1().PodGroups(app.Namespace).Create(
			context.TODO(), &podGroup, metav1.CreateOptions{},
		); err != nil {
			if errors.IsAlreadyExists(err) {
				v.log.Info("pod group already exists, no need to create")
				return nil
			}

			v.log.Error(err, "Pod group CREATE error!", "PodGroup.Error", err)
			return err
		}
	} else {
		if pg.Spec.MinMember != size || !quotav1.Equals(*pg.Spec.MinResources, totalResource) {
			pg.Spec.MinMember = size
			pg.Spec.MinResources = &totalResource
			if _, err := v.volcanoClient.SchedulingV1beta1().PodGroups(app.Namespace).Update(
				context.TODO(), pg, metav1.UpdateOptions{},
			); err != nil {
				v.log.Error(err, "Pod group UPDATE error!", "podGroup", podGroupName)
				return err
			}
		}
	}
	return nil
}

func createPodGroup(
	app *rayv1alpha1.RayCluster,
	podGroupName string,
	size int32,
	totalResource corev1.ResourceList,
) v1beta1.PodGroup {
	podGroup := v1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: app.Namespace,
			Name:      podGroupName,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(app, rayv1alpha1.SchemeGroupVersion.WithKind("RayCluster")),
			},
		},
		Spec: v1beta1.PodGroupSpec{
			MinMember:    size,
			MinResources: &totalResource,
		},
		Status: v1beta1.PodGroupStatus{
			Phase: v1beta1.PodGroupPending,
		},
	}

	if queue, ok := app.ObjectMeta.Labels[QueueNameLabelKey]; ok {
		podGroup.Spec.Queue = queue
	}

	if priorityClassName, ok := app.ObjectMeta.Labels[common.RayPriorityClassName]; ok {
		podGroup.Spec.PriorityClassName = priorityClassName
	}

	return podGroup
}

func (v *VolcanoBatchScheduler) AddMetadataToPod(app *rayv1alpha1.RayCluster, pod *corev1.Pod) {
	pod.Annotations[v1beta1.KubeGroupNameAnnotationKey] = v.getAppPodGroupName(app)
	if queue, ok := app.ObjectMeta.Labels[QueueNameLabelKey]; ok {
		pod.Labels[QueueNameLabelKey] = queue
	}
	if priorityClassName, ok := app.ObjectMeta.Labels[common.RayPriorityClassName]; ok {
		pod.Spec.PriorityClassName = priorityClassName
	}
	pod.Spec.SchedulerName = v.Name()
}

func (vf *VolcanoBatchSchedulerFactory) New(config *rest.Config) (schedulerinterface.BatchScheduler, error) {
	vkClient, err := volcanoclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize volcano client with error %v", err)
	}

	extClient, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize k8s extension client with error %v", err)
	}

	if _, err := extClient.ApiextensionsV1().CustomResourceDefinitions().Get(
		context.TODO(),
		PodGroupName,
		metav1.GetOptions{},
	); err != nil {
		if _, err := extClient.ApiextensionsV1beta1().CustomResourceDefinitions().Get(
			context.TODO(),
			PodGroupName,
			metav1.GetOptions{},
		); err != nil {
			return nil, fmt.Errorf("podGroup CRD is required to exist in current cluster. error: %s", err)
		}
	}
	return &VolcanoBatchScheduler{
		extensionClient: extClient,
		volcanoClient:   vkClient,
		log:             logf.Log.WithName("volcano"),
	}, nil
}

func (vf *VolcanoBatchSchedulerFactory) AddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(v1beta1.AddToScheme(scheme))
}

func (vf *VolcanoBatchSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b.
		Watches(&source.Kind{Type: &v1beta1.PodGroup{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &rayv1alpha1.RayCluster{},
		})
}
