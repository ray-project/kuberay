package v1alpha1

import (
	"fmt"

	"github.com/go-logr/logr"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/volcano"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/yunikorn"
)

func ValidateBatchSchedulerConfig(logger logr.Logger, config Configuration) error {
	if config.EnableBatchScheduler && len(config.BatchScheduler) > 0 {
		return fmt.Errorf("both feature flags enable-batch-scheduler (deprecated) and batch-scheduler are set. Please use batch-scheduler only")
	}

	if config.EnableBatchScheduler {
		logger.Info("Feature flag enable-batch-scheduler is deprecated and will not be supported soon. " +
			"Use batch-scheduler instead. ")
		return nil
	}

	if len(config.BatchScheduler) > 0 {
		// if a customized scheduler is configured, check it is supported
		if config.BatchScheduler == volcano.GetPluginName() || config.BatchScheduler == yunikorn.GetPluginName() {
			logger.Info("Feature flag batch-scheduler is enabled",
				"scheduler name", config.BatchScheduler)
		} else {
			return fmt.Errorf("scheduler is not supported, name=%s", config.BatchScheduler)
		}
	}

	return nil
}
