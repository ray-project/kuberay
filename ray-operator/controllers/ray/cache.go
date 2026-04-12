/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ray

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// CacheSelectors returns the label selectors used to scope the controller-runtime
// informer cache to only KubeRay-managed resources.
//
// Jobs are selected by app.kubernetes.io/created-by=kuberay-operator.
// Pods are selected by the existence of ray.io/node-type instead of the created-by label,
// because user-provided pod template labels can override created-by, which would
// cause those Pods to disappear from the cache. The ray.io/node-type label is
// protected from user override in labelPod().
func CacheSelectors() (map[client.Object]cache.ByObject, error) {
	createdByLabel, err := labels.NewRequirement(utils.KubernetesCreatedByLabelKey, selection.Equals, []string{utils.ComponentName})
	if err != nil {
		return nil, err
	}
	jobSelector := labels.NewSelector().Add(*createdByLabel)

	rayNodeLabel, err := labels.NewRequirement(utils.RayNodeTypeLabelKey, selection.Exists, []string{})
	if err != nil {
		return nil, err
	}
	podSelector := labels.NewSelector().Add(*rayNodeLabel)

	return map[client.Object]cache.ByObject{
		&batchv1.Job{}: {Label: jobSelector},
		&corev1.Pod{}:  {Label: podSelector},
	}, nil
}
