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

package batchscheduler

import (
	"fmt"
	"sync"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/pkg/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/pkg/batchscheduler/volcano"
)

var schedulerContainers = map[string]schedulerinterface.BatchSchedulerFactory{
	schedulerinterface.GetDefaultPluginName(): &schedulerinterface.DefaultBatchSchedulerFactory{},
	volcano.GetPluginName():                   &volcano.VolcanoBatchSchedulerFactory{},
}

func GetRegisteredNames() []string {
	var pluginNames []string
	for key := range schedulerContainers {
		pluginNames = append(pluginNames, key)
	}
	return pluginNames
}

func ConfigureReconciler(b *builder.Builder) *builder.Builder {
	for _, factory := range schedulerContainers {
		b = factory.ConfigureReconciler(b)
	}
	return b
}

type SchedulerManager struct {
	sync.Mutex
	config  *rest.Config
	plugins map[string]schedulerinterface.BatchScheduler
}

func NewSchedulerManager(config *rest.Config) *SchedulerManager {
	manager := SchedulerManager{
		config:  config,
		plugins: make(map[string]schedulerinterface.BatchScheduler),
	}
	return &manager
}

func (batch *SchedulerManager) GetSchedulerForCluster(app *rayiov1alpha1.RayCluster) (schedulerinterface.BatchScheduler, error) {
	if schedulerName, ok := app.ObjectMeta.Labels[common.RaySchedulerName]; ok {
		return batch.GetScheduler(schedulerName)
	}

	// no scheduler provided
	return &schedulerinterface.DefaultBatchScheduler{}, nil
}

func (batch *SchedulerManager) GetScheduler(schedulerName string) (schedulerinterface.BatchScheduler, error) {
	factory, registered := schedulerContainers[schedulerName]
	if !registered {
		return nil, fmt.Errorf("unregistered scheduler plugin %s", schedulerName)
	}

	batch.Lock()
	defer batch.Unlock()

	if plugin, existed := batch.plugins[schedulerName]; existed && plugin != nil {
		return plugin, nil
	} else if existed && plugin == nil {
		return nil, fmt.Errorf(
			"failed to get scheduler plugin %s, previous initialization has failed", schedulerName)
	} else {
		if plugin, err := factory.New(batch.config); err != nil {
			batch.plugins[schedulerName] = nil
			return nil, err
		} else {
			batch.plugins[schedulerName] = plugin
			return plugin, nil
		}
	}
}
