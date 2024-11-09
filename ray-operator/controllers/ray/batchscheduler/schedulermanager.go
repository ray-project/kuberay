package batchscheduler

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/volcano"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/yunikorn"

	"k8s.io/client-go/rest"

	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
)

type SchedulerManager struct {
	config     *rest.Config
	factory    schedulerinterface.BatchSchedulerFactory
	scheduler  schedulerinterface.BatchScheduler
	rayConfigs configapi.Configuration
	sync.Mutex
}

// NewSchedulerManager maintains a specific scheduler plugin based on config
func NewSchedulerManager(rayConfigs configapi.Configuration, config *rest.Config) (*SchedulerManager, error) {
	// init the scheduler factory from config
	factory, err := getSchedulerFactory(rayConfigs)
	if err != nil {
		return nil, err
	}

	scheduler, err := factory.New(config)
	if err != nil {
		return nil, err
	}

	manager := SchedulerManager{
		rayConfigs: rayConfigs,
		config:     config,
		factory:    factory,
		scheduler:  scheduler,
	}

	return &manager, nil
}

func getSchedulerFactory(rayConfigs configapi.Configuration) (schedulerinterface.BatchSchedulerFactory, error) {
	var factory schedulerinterface.BatchSchedulerFactory

	// when a batch scheduler name is provided
	// only support a white list of names, empty value is the default value
	// it throws error if an unknown name is provided
	if len(rayConfigs.BatchScheduler) > 0 {
		switch rayConfigs.BatchScheduler {
		case volcano.GetPluginName():
			factory = &volcano.VolcanoBatchSchedulerFactory{}
		case yunikorn.GetPluginName():
			factory = &yunikorn.YuniKornSchedulerFactory{}
		default:
			return nil, fmt.Errorf("the scheduler is not supported, name=%s", rayConfigs.BatchScheduler)
		}
	} else {
		// empty is the default value, when not set
		// use DefaultBatchSchedulerFactory, it's a no-opt factory
		factory = &schedulerinterface.DefaultBatchSchedulerFactory{}
	}

	// legacy option, if this is enabled, register volcano
	// this is for backward compatibility
	if rayConfigs.EnableBatchScheduler {
		factory = &volcano.VolcanoBatchSchedulerFactory{}
	}

	return factory, nil
}

func (batch *SchedulerManager) GetSchedulerForCluster() (schedulerinterface.BatchScheduler, error) {
	return batch.scheduler, nil
}

func (batch *SchedulerManager) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	batch.factory.ConfigureReconciler(b)
	return b
}

func (batch *SchedulerManager) AddToScheme(scheme *runtime.Scheme) {
	batch.factory.AddToScheme(scheme)
}
