package yunikorn

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
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
	yk.populatePodLabels(rayCluster1, rayPod, RayClusterApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	yk.populatePodLabels(rayCluster1, rayPod, RayClusterQueueLabelName, YuniKornPodQueueLabelName)
	assert.Equal(t, podLabelsContains(rayPod, YuniKornPodApplicationIDLabelName, job1), true)
	assert.Equal(t, podLabelsContains(rayPod, YuniKornPodQueueLabelName, queue1), true)

	// --- case 2
	// Ray Cluster CR has nothing
	// In this case, the pod will not be populated with the required labels
	job2 := "job-2-01234"
	queue2 := "root.default"

	rayCluster2 := createRayClusterWithLabels(
		"ray-cluster-without-labels",
		"test1",
		nil, // empty labels
	)
	rayPod3 := createPod("my-pod-2", "test")
	yk.populatePodLabels(rayCluster2, rayPod3, RayClusterApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	yk.populatePodLabels(rayCluster2, rayPod3, RayClusterQueueLabelName, YuniKornPodQueueLabelName)
	assert.Equal(t, podLabelsContains(rayPod3, YuniKornPodApplicationIDLabelName, job2), false)
	assert.Equal(t, podLabelsContains(rayPod3, YuniKornPodQueueLabelName, queue2), false)
}

func TestIsGangSchedulingEnabled(t *testing.T) {
	yk := &YuniKornScheduler{}

	job1 := "job-1-01234"
	queue1 := "root.default"
	rayCluster1 := createRayClusterWithLabels(
		"ray-cluster-with-gang-scheduling",
		"test1",
		map[string]string{
			RayClusterApplicationIDLabelName:  job1,
			RayClusterQueueLabelName:          queue1,
			RayClusterGangSchedulingLabelName: "true",
		},
	)

	assert.Equal(t, yk.isGangSchedulingEnabled(rayCluster1), true)

	rayCluster2 := createRayClusterWithLabels(
		"ray-cluster-with-gang-scheduling",
		"test2",
		map[string]string{
			RayClusterApplicationIDLabelName:  job1,
			RayClusterQueueLabelName:          queue1,
			RayClusterGangSchedulingLabelName: "",
		},
	)

	assert.Equal(t, yk.isGangSchedulingEnabled(rayCluster2), true)

	rayCluster3 := createRayClusterWithLabels(
		"ray-cluster-with-gang-scheduling",
		"test3",
		map[string]string{
			RayClusterApplicationIDLabelName: job1,
			RayClusterQueueLabelName:         queue1,
		},
	)

	assert.Equal(t, yk.isGangSchedulingEnabled(rayCluster3), false)
}

func TestPopulateGangSchedulingAnnotations(t *testing.T) {
	yk := &YuniKornScheduler{}

	job1 := "job-1-01234"
	queue1 := "root.default"

	// test the case when gang-scheduling is enabled
	rayClusterWithGangScheduling := createRayClusterWithLabels(
		"ray-cluster-with-gang-scheduling",
		"test3",
		map[string]string{
			RayClusterApplicationIDLabelName:  job1,
			RayClusterQueueLabelName:          queue1,
			RayClusterGangSchedulingLabelName: "true",
		},
	)

	// head pod:
	//   cpu: 5
	//   memory: 5Gi
	addHeadPodSpec(rayClusterWithGangScheduling, v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("5"),
		v1.ResourceMemory: resource.MustParse("5Gi"),
	})

	// worker pod:
	//   cpu: 2
	//   memory: 10Gi
	//   nvidia.com/gpu: 1
	addWorkerPodSpec(rayClusterWithGangScheduling,
		"worker-group-1", 1, 1, 2, v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("2"),
			v1.ResourceMemory: resource.MustParse("10Gi"),
			"nvidia.com/gpu":  resource.MustParse("1"),
		})

	// gang-scheduling enabled case, the plugin should populate the taskGroup annotation to the app
	rayPod := createPod("ray-pod", "default")
	err := yk.populateTaskGroupsAnnotationToPod(rayClusterWithGangScheduling, rayPod)
	assert.NoError(t, err, "failed to populate task groups annotation to pod")

	kk, err := GetTaskGroupsFromAnnotation(rayPod)
	assert.NoError(t, err)
	assert.Equal(t, len(kk), 2)
	// verify the annotation value
	taskGroupsSpec := rayPod.Annotations[YuniKornTaskGroupsAnnotationName]
	assert.Equal(t, true, len(taskGroupsSpec) > 0)
	taskGroups := newTaskGroups()
	err = taskGroups.unmarshalFrom(taskGroupsSpec)
	assert.NoError(t, err)
	assert.Equal(t, len(taskGroups.Groups), 2)

	// verify the correctness of head group
	headGroup := taskGroups.getTaskGroup(utils.RayNodeHeadGroupLabelValue)
	assert.NotNil(t, headGroup)
	assert.Equal(t, int32(1), headGroup.MinMember)
	assert.Equal(t, resource.MustParse("5"), headGroup.MinResource[v1.ResourceCPU.String()])
	assert.Equal(t, resource.MustParse("5Gi"), headGroup.MinResource[v1.ResourceMemory.String()])

	// verify the correctness of worker group
	workerGroup := taskGroups.getTaskGroup("worker-group-1")
	assert.NotNil(t, workerGroup)
	assert.Equal(t, int32(1), workerGroup.MinMember)
	assert.Equal(t, resource.MustParse("2"), workerGroup.MinResource[v1.ResourceCPU.String()])
	assert.Equal(t, resource.MustParse("10Gi"), workerGroup.MinResource[v1.ResourceMemory.String()])
	assert.Equal(t, resource.MustParse("1"), workerGroup.MinResource["nvidia.com/gpu"])
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

func addHeadPodSpec(app *rayv1.RayCluster, resource v1.ResourceList) {
	// app.Spec.HeadGroupSpec.Template.Spec.Containers
	headContainers := []v1.Container{
		{
			Name:  "head-pod",
			Image: "ray.io/ray-head:latest",
			Resources: v1.ResourceRequirements{
				Limits:   nil,
				Requests: resource,
			},
		},
	}

	app.Spec.HeadGroupSpec.Template.Spec.Containers = headContainers
}

func addWorkerPodSpec(app *rayv1.RayCluster, workerGroupName string,
	replicas int32, minReplicas int32, maxReplicas int32, resources v1.ResourceList,
) {
	workerContainers := []v1.Container{
		{
			Name:  "worker-pod",
			Image: "ray.io/ray-head:latest",
			Resources: v1.ResourceRequirements{
				Limits:   nil,
				Requests: resources,
			},
		},
	}

	if len(app.Spec.WorkerGroupSpecs) == 0 {
		app.Spec.WorkerGroupSpecs = make([]rayv1.WorkerGroupSpec, 0)
	}

	app.Spec.WorkerGroupSpecs = append(app.Spec.WorkerGroupSpecs, rayv1.WorkerGroupSpec{
		GroupName:   workerGroupName,
		Replicas:    &replicas,
		MinReplicas: &minReplicas,
		MaxReplicas: &maxReplicas,
		Template: v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: workerContainers,
			},
		},
	})
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

func GetTaskGroupsFromAnnotation(pod *v1.Pod) ([]TaskGroup, error) {
	taskGroupInfo, exist := pod.Annotations[YuniKornTaskGroupsAnnotationName]
	if !exist {
		return nil, fmt.Errorf("not found")
	}

	taskGroups := []TaskGroup{}
	err := json.Unmarshal([]byte(taskGroupInfo), &taskGroups)
	if err != nil {
		return nil, err
	}
	// json.Unmarshal won't return error if name or MinMember is empty, but will return error if MinResource is empty or error format.
	for _, taskGroup := range taskGroups {
		if taskGroup.Name == "" {
			return nil, fmt.Errorf("can't get taskGroup Name from pod annotation, %s",
				taskGroupInfo)
		}
		if taskGroup.MinResource == nil {
			return nil, fmt.Errorf("can't get taskGroup MinResource from pod annotation, %s",
				taskGroupInfo)
		}
		if taskGroup.MinMember == int32(0) {
			return nil, fmt.Errorf("can't get taskGroup MinMember from pod annotation, %s",
				taskGroupInfo)
		}
		if taskGroup.MinMember < int32(0) {
			return nil, fmt.Errorf("minMember cannot be negative, %s",
				taskGroupInfo)
		}
	}
	return taskGroups, nil
}
