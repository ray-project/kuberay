/*
Copyright 2019 Google LLC
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    https://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package schedulerinterface

import (
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

type BatchScheduler interface {
	Name() string
	DoBatchSchedulingOnSubmission(app *rayiov1alpha1.RayCluster) error
	AddMetadataToPod(app *rayiov1alpha1.RayCluster, pod *v1.Pod)
	CleanupOnCompletion(app *rayiov1alpha1.RayCluster) error
}

type BatchSchedulerFactory interface {
	New(config *rest.Config) (BatchScheduler, error)
	ConfigureReconciler(b *builder.Builder) *builder.Builder
}

type DefaultBatchScheduler struct{}

type DefaultBatchSchedulerFactory struct{}

func GetDefaultPluginName() string {
	return "default"
}

func (d *DefaultBatchScheduler) Name() string {
	return GetDefaultPluginName()
}

func (d *DefaultBatchScheduler) DoBatchSchedulingOnSubmission(app *rayiov1alpha1.RayCluster) error {
	return nil
}

func (d *DefaultBatchScheduler) AddMetadataToPod(app *rayiov1alpha1.RayCluster, pod *v1.Pod) {
}

func (d *DefaultBatchScheduler) CleanupOnCompletion(app *rayiov1alpha1.RayCluster) error {
	return nil
}

func (df *DefaultBatchSchedulerFactory) New(config *rest.Config) (BatchScheduler, error) {
	return &DefaultBatchScheduler{}, nil
}

func (df *DefaultBatchSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
