package yunikorn

import (
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestPopulatePodLabels(t *testing.T) {
	yk := &YuniKornScheduler{}

	// --- case 1
	// Ray Cluster CR has labels defined
	job1 := "job-1-01234"
	queue1 := "root.default"

	rayCluster1 := createRayClusterWithLabels(
		"ray-cluster-with-labels",
		"test",
		map[string]string{
			RayClusterApplicationIDLabelName: job1,
			RayClusterQueueLabelName:         queue1,
		},
	)

	rayPod := createPod("my-pod-1", "test")
	yk.populatePodLabels(rayCluster1, rayPod, RayClusterApplicationIDLabelName, PodApplicationIDLabelName)
	yk.populatePodLabels(rayCluster1, rayPod, RayClusterQueueLabelName, PodQueueLabelName)
	assert.Equal(t, podLabelsContains(rayPod, PodApplicationIDLabelName, job1), true)
	assert.Equal(t, podLabelsContains(rayPod, PodQueueLabelName, queue1), true)

	// --- case 2
	// Ray Cluster CR has nothing
	// In this case, the pod will not be populated with the required labels
	job2 := "job-2-01234"
	queue2 := "root.default"

	rayCluster2 := createRayClusterWithLabels(
		"ray-cluster-without-labels",
		"test",
		nil, // empty annotations
	)
	rayPod3 := createPod("my-pod-2", "test")
	yk.populatePodLabels(rayCluster2, rayPod3, RayClusterApplicationIDLabelName, PodApplicationIDLabelName)
	yk.populatePodLabels(rayCluster2, rayPod3, RayClusterQueueLabelName, PodQueueLabelName)
	assert.Equal(t, podLabelsContains(rayPod3, PodApplicationIDLabelName, job2), false)
	assert.Equal(t, podLabelsContains(rayPod3, PodQueueLabelName, queue2), false)
}

func createRayClusterWithLabels(name string, namespace string, labels map[string]string) *rayv1.RayCluster {
	rayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
	}

	return rayCluster
}

func createPod(name string, namespace string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
		},
	}
}

func podLabelsContains(pod *v1.Pod, key string, value string) bool {
	if pod == nil {
		return false
	}

	if len(pod.Labels) > 0 {
		labelValue, exist := pod.Labels[key]
		if exist {
			if labelValue == value {
				return true
			}
		}
	}

	return false
}
