package v1alpha1

import (
	"fmt"

	"github.com/go-logr/logr"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/volcano"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/yunikorn"
)

func ValidateBatchSchedulerConfig(logger logr.Logger, config Configuration) error {
	if config.EnableBatchScheduler {
		if len(config.BatchScheduler) > 0 {
			return fmt.Errorf("invalid configuration found, " +
				"do not use both options together: \"batch-scheduler\" and \"enable-batch-scheduler\"")
		}
		logger.Info("Feature flag enable-batch-scheduler is deprecated and will not be supported soon. " +
			"Use batch-scheduler instead. ")
	}
	if len(config.BatchScheduler) > 0 {
		// default option, no-opt.
		if config.BatchScheduler == "default" {
			return nil
		}

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
