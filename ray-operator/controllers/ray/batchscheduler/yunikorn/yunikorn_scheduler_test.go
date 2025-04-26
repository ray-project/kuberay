package yunikorn

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func TestPopulatePodLabels(t *testing.T) {
	yk := &YuniKornScheduler{}
	ctx := context.Background()

	testCases := []struct {
		name                   string
		job                    string
		queue                  string
		clusterName            string
		clusterNameSpace       string
		clusterLabel           map[string]string
		podName                string
		expectJobLabelResult   bool
		expectQueueLabelResult bool
	}{
		{
			name:             "Ray Cluster CR has labels defined",
			job:              "job-1-01234",
			queue:            "root.default",
			clusterName:      "ray-cluster-with-labels",
			clusterNameSpace: "test",
			clusterLabel: map[string]string{
				RayClusterApplicationIDLabelName: "job-1-01234",
				RayClusterQueueLabelName:         "root.default",
			},
			podName:                "my-pod-1",
			expectJobLabelResult:   true,
			expectQueueLabelResult: true,
		},
		{
			name:                   "Ray Cluster CR has nothing. In this case, the pod will not be populated with the required labels",
			job:                    "job-2-01234",
			queue:                  "root.default",
			clusterName:            "ray-cluster-with-labels",
			clusterNameSpace:       "test1",
			clusterLabel:           nil,
			podName:                "my-pod-2",
			expectJobLabelResult:   false,
			expectQueueLabelResult: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rayCluster := createRayClusterWithLabels(testCase.clusterName, testCase.clusterNameSpace, testCase.clusterLabel)
			rayPod := createPod(testCase.podName, testCase.clusterNameSpace)
			yk.populatePodLabels(ctx, rayCluster, rayPod, RayClusterApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
			yk.populatePodLabels(ctx, rayCluster, rayPod, RayClusterQueueLabelName, YuniKornPodQueueLabelName)
			assert.Equal(t, podLabelsContains(rayPod, YuniKornPodApplicationIDLabelName, testCase.job), testCase.expectJobLabelResult)
			assert.Equal(t, podLabelsContains(rayPod, YuniKornPodQueueLabelName, testCase.queue), testCase.expectQueueLabelResult)
		})
	}
}

func TestIsGangSchedulingEnabled(t *testing.T) {
	yk := &YuniKornScheduler{}

	job1 := "job-1-01234"
	queue1 := "root.default"
	rayCluster1 := createRayClusterWithLabels(
		"ray-cluster-with-gang-scheduling",
		"test1",
		map[string]string{
			RayClusterApplicationIDLabelName:      job1,
			RayClusterQueueLabelName:              queue1,
			utils.RayClusterGangSchedulingEnabled: "true",
		},
	)

	assert.True(t, yk.isGangSchedulingEnabled(rayCluster1))

	rayCluster2 := createRayClusterWithLabels(
		"ray-cluster-with-gang-scheduling",
		"test2",
		map[string]string{
			RayClusterApplicationIDLabelName:      job1,
			RayClusterQueueLabelName:              queue1,
			utils.RayClusterGangSchedulingEnabled: "",
		},
	)

	assert.True(t, yk.isGangSchedulingEnabled(rayCluster2))

	rayCluster3 := createRayClusterWithLabels(
		"ray-cluster-with-gang-scheduling",
		"test3",
		map[string]string{
			RayClusterApplicationIDLabelName: job1,
			RayClusterQueueLabelName:         queue1,
		},
	)

	assert.False(t, yk.isGangSchedulingEnabled(rayCluster3))
}

func TestPopulateGangSchedulingAnnotations(t *testing.T) {
	yk := &YuniKornScheduler{}
	ctx := context.Background()

	job1 := "job-1-01234"
	queue1 := "root.default"

	// test the case when gang-scheduling is enabled
	rayClusterWithGangScheduling := createRayClusterWithLabels(
		"ray-cluster-with-gang-scheduling",
		"test3",
		map[string]string{
			RayClusterApplicationIDLabelName:      job1,
			RayClusterQueueLabelName:              queue1,
			utils.RayClusterGangSchedulingEnabled: "true",
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
	yk.populateTaskGroupsAnnotationToPod(ctx, rayClusterWithGangScheduling, rayPod)

	kk, err := getTaskGroupsFromAnnotation(rayPod)
	require.NoError(t, err)
	assert.Len(t, kk, 2)
	// verify the annotation value
	taskGroupsSpec := rayPod.Annotations[YuniKornTaskGroupsAnnotationName]
	assert.NotEmpty(t, taskGroupsSpec)
	taskGroups := newTaskGroups()
	err = taskGroups.unmarshalFrom(taskGroupsSpec)
	require.NoError(t, err)
	assert.Len(t, taskGroups.Groups, 2)

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

func getTaskGroupsFromAnnotation(pod *v1.Pod) ([]TaskGroup, error) {
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
