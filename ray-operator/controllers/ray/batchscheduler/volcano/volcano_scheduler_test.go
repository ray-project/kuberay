package volcano

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func createTestRayCluster(numOfHosts int32) rayv1.RayCluster {
	headSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name: "ray-head",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("256m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
			},
		},
	}

	workerSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name: "ray-worker",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
						"nvidia.com/gpu":      resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("256m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
			},
		},
	}

	return rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: headSpec,
				},
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					Template: corev1.PodTemplateSpec{
						Spec: workerSpec,
					},
					Replicas:    ptr.To[int32](2),
					NumOfHosts:  numOfHosts,
					MinReplicas: ptr.To[int32](1),
					MaxReplicas: ptr.To[int32](4),
				},
			},
		},
	}
}

func TestCreatePodGroup(t *testing.T) {
	a := assert.New(t)

	cluster := createTestRayCluster(1)

	minMember := utils.CalculateDesiredReplicas(context.Background(), &cluster) + 1
	totalResource := utils.CalculateDesiredResources(&cluster)
	pg := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)

	a.Equal(cluster.Namespace, pg.Namespace)

	// 1 head + 2 workers (desired, not min replicas)
	a.Equal(int32(3), pg.Spec.MinMember)

	// 256m * 3 (requests, not limits)
	a.Equal("768m", pg.Spec.MinResources.Cpu().String())

	// 256Mi * 3 (requests, not limits)
	a.Equal("768Mi", pg.Spec.MinResources.Memory().String())

	// 2 GPUs total
	a.Equal("2", pg.Spec.MinResources.Name("nvidia.com/gpu", resource.BinarySI).String())
}

func TestCreatePodGroup_NumOfHosts2(t *testing.T) {
	a := assert.New(t)

	cluster := createTestRayCluster(2)

	minMember := utils.CalculateDesiredReplicas(context.Background(), &cluster) + 1
	totalResource := utils.CalculateDesiredResources(&cluster)
	pg := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)

	a.Equal(cluster.Namespace, pg.Namespace)

	// 2 workers (desired, not min replicas) * 2 (num of hosts) + 1 head
	// 2 * 2 + 1 = 5
	a.Equal(int32(5), pg.Spec.MinMember)

	// 256m * (2 (requests, not limits) * 2 (num of hosts) + 1 head)
	// 256m * 5 = 1280m
	a.Equal("1280m", pg.Spec.MinResources.Cpu().String())

	// 256Mi * (2 (requests, not limits) * 2 (num of hosts) + 1 head)
	// 256Mi * 5 = 1280Mi
	a.Equal("1280Mi", pg.Spec.MinResources.Memory().String())

	// 2 GPUs * 2 (num of hosts) total
	// 2 GPUs * 2 = 4 GPUs
	a.Equal("4", pg.Spec.MinResources.Name("nvidia.com/gpu", resource.BinarySI).String())
}
