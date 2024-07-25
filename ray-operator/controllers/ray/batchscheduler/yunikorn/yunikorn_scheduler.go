package yunikorn

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
)

const (
	SchedulerName                     string = "yunikorn"
	YuniKornPodApplicationIDLabelName string = "applicationId"
	YuniKornPodQueueLabelName         string = "queue"
	RayClusterApplicationIDLabelName  string = "yunikorn.apache.org/application-id"
	RayClusterQueueLabelName          string = "yunikorn.apache.org/queue-name"
)

type YuniKornScheduler struct {
	log logr.Logger
}

type YuniKornSchedulerFactory struct{}

func GetPluginName() string {
	return SchedulerName
}

func (y *YuniKornScheduler) Name() string {
	return GetPluginName()
}

func (y *YuniKornScheduler) DoBatchSchedulingOnSubmission(_ context.Context, _ *rayv1.RayCluster) error {
	// yunikorn doesn't require any resources to be created upfront
	// this is a no-opt for this implementation
	return nil
}

func (y *YuniKornScheduler) populatePodLabels(app *rayv1.RayCluster, pod *corev1.Pod, sourceKey string, targetKey string) {
	// check labels
	if value, exist := app.Labels[sourceKey]; exist {
		y.log.Info("Updating pod label based on RayCluster annotations",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		pod.Labels[targetKey] = value
	}
}

func (y *YuniKornScheduler) AddMetadataToPod(app *rayv1.RayCluster, _ string, pod *corev1.Pod) {
	y.populatePodLabels(app, pod, RayClusterApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	y.populatePodLabels(app, pod, RayClusterQueueLabelName, YuniKornPodQueueLabelName)
	pod.Spec.SchedulerName = y.Name()
}

func (yf *YuniKornSchedulerFactory) New(_ *rest.Config) (schedulerinterface.BatchScheduler, error) {
	return &YuniKornScheduler{
		log: logf.Log.WithName(SchedulerName),
	}, nil
}

func (yf *YuniKornSchedulerFactory) AddToScheme(_ *runtime.Scheme) {
	// No extra scheme needs to be registered
}

func (yf *YuniKornSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
