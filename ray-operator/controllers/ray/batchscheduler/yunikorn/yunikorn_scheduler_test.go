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

	rayCluster1 := createRayClusterWithLabelsAndAnnotations(
		"ray-cluster-with-labels",
		"test",
		map[string]string{
			RayClusterApplicationIDLabelName: job1,
			RayClusterQueueLabelName:         queue1,
		},
		nil, // empty annotations
	)

	rayPod := createPod("my-pod-1", "test")
	yk.populatePodLabels(rayCluster1, rayPod, RayClusterApplicationIDLabelName, PodApplicationIDLabelName)
	yk.populatePodLabels(rayCluster1, rayPod, RayClusterQueueLabelName, PodQueueLabelName)
	assert.Equal(t, podLabelContains(rayPod, PodApplicationIDLabelName, job1), true)
	assert.Equal(t, podLabelContains(rayPod, PodQueueLabelName, queue1), true)

	// --- case 2
	// Ray Cluster CR has annotations defined
	job2 := "job-2-01234"
	queue2 := "root.default"

	rayCluster2 := createRayClusterWithLabelsAndAnnotations(
		"ray-cluster-with-annotations",
		"test",
		nil, // empty labels
		map[string]string{
			RayClusterApplicationIDLabelName: job2,
			RayClusterQueueLabelName:         queue2,
		},
	)

	rayPod2 := createPod("my-pod-2", "test")
	yk.populatePodLabels(rayCluster2, rayPod2, RayClusterApplicationIDLabelName, PodApplicationIDLabelName)
	yk.populatePodLabels(rayCluster2, rayPod2, RayClusterQueueLabelName, PodQueueLabelName)
	assert.Equal(t, podLabelContains(rayPod2, PodApplicationIDLabelName, job2), true)
	assert.Equal(t, podLabelContains(rayPod2, PodQueueLabelName, queue2), true)

	// --- case 3
	// Ray Cluster CR has nothing
	// In this case, the pod will not be populated with the required labels
	job3 := "job-3-01234"
	queue3 := "root.default"

	rayCluster3 := createRayClusterWithLabelsAndAnnotations(
		"ray-cluster-with-labels",
		"test",
		nil, // empty labels
		nil, // empty annotations
	)
	rayPod3 := createPod("my-pod-3", "test")
	yk.populatePodLabels(rayCluster3, rayPod3, RayClusterApplicationIDLabelName, PodApplicationIDLabelName)
	yk.populatePodLabels(rayCluster3, rayPod3, RayClusterQueueLabelName, PodQueueLabelName)
	assert.Equal(t, podLabelContains(rayPod3, PodApplicationIDLabelName, job3), false)
	assert.Equal(t, podLabelContains(rayPod3, PodQueueLabelName, queue3), false)
}

func createRayClusterWithLabelsAndAnnotations(name string, namespace string,
	labels map[string]string, annotations map[string]string) *rayv1.RayCluster {
	rayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
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

func podLabelContains(pod *v1.Pod, key string, value string) bool {
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
