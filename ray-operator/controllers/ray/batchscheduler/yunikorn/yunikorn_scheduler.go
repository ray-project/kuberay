package yunikorn

import (
	"context"
	"github.com/go-logr/logr"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	SchedulerName                    string = "yunikorn"
	PodApplicationIDLabelName               = "applicationId"
	PodQueueLabelName                       = "queue"
	RayClusterApplicationIDLabelName        = "yunikorn.apache.org/application-id"
	RayClusterQueueLabelName                = "yunikorn.apache.org/queue-name"
)

type YuniKornScheduler struct {
	log logr.Logger
}

type YuniKornSchedulerFactory struct{}

func GetPluginName() string {
	return SchedulerName
}

func (y *YuniKornScheduler) Name() string {
	return SchedulerName
}

func (y *YuniKornScheduler) DoBatchSchedulingOnSubmission(ctx context.Context, app *rayv1.RayCluster) error {
	// yunikorn doesn't require any resources to be created upfront
	// this is a no-opt for this implementation
	return nil
}

func (y *YuniKornScheduler) populatePodLabels(app *rayv1.RayCluster, pod *v1.Pod, sourceKey string, targetKey string) {
	// check labels
	if value, exist := app.Labels[sourceKey]; exist {
		y.log.Info("Updating pod label based on RayCluster labels",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		pod.Labels[targetKey] = value
	}

	// check annotations
	if value, exist := app.Annotations[sourceKey]; exist {
		y.log.Info("Updating pod label based on RayCluster annotations",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		pod.Labels[targetKey] = value
	}
}

func (y *YuniKornScheduler) AddMetadataToPod(app *rayv1.RayCluster, groupName string, pod *v1.Pod) {
	y.populatePodLabels(app, pod, RayClusterApplicationIDLabelName, PodApplicationIDLabelName)
	y.populatePodLabels(app, pod, RayClusterQueueLabelName, PodQueueLabelName)
	pod.Spec.SchedulerName = y.Name()
}

func (yf *YuniKornSchedulerFactory) New(config *rest.Config) (schedulerinterface.BatchScheduler, error) {
	return &YuniKornScheduler{
		log: logf.Log.WithName(SchedulerName),
	}, nil
}

func (yf *YuniKornSchedulerFactory) AddToScheme(scheme *runtime.Scheme) {
	return
}

func (yf *YuniKornSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
