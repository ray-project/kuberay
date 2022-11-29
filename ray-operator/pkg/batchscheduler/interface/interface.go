package schedulerinterface

import (
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

type BatchScheduler interface {
	Name() string

	ShouldSchedule(app *rayiov1alpha1.RayCluster) bool
	DoBatchSchedulingOnSubmission(app *rayiov1alpha1.RayCluster) error
	CleanupOnCompletion(app *rayiov1alpha1.RayCluster) error
}
