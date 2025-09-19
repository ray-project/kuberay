package volcano

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	volcanobatchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	volcanoschedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

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

func createTestRayJob(numOfHosts int32) rayv1.RayJob {
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
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("256m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
			},
		},
	}

	return rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayjob-sample",
			Namespace: "default",
			Labels: map[string]string{
				QueueNameLabelKey:          "test-queue",
				utils.RayPriorityClassName: "high-priority",
			},
		},
		Spec: rayv1.RayJobSpec{
			RayClusterSpec: &rayv1.RayClusterSpec{
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
		},
	}
}

func createTestRayClusterFromRayJob(rayJobName string) rayv1.RayCluster {
	cluster := createTestRayCluster(1)
	cluster.Name = "raycluster-from-rayjob"
	if cluster.Labels == nil {
		cluster.Labels = make(map[string]string)
	}
	cluster.Labels[utils.RayOriginatedFromCRDLabelKey] = utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD)
	cluster.Labels[utils.RayOriginatedFromCRNameLabelKey] = rayJobName
	return cluster
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

func TestCreatePodGroupForRayJob(t *testing.T) {
	a := assert.New(t)

	rayJob := createTestRayJob(1)

	// Create RayCluster from RayJob spec for calculation
	rayCluster := &rayv1.RayCluster{
		Spec: *rayJob.Spec.RayClusterSpec,
	}

	minMember := utils.CalculateDesiredReplicas(context.Background(), rayCluster) + 1
	totalResource := utils.CalculateDesiredResources(rayCluster)
	pg := createPodGroup(&rayJob, getAppPodGroupName(&rayJob), minMember, totalResource)

	a.Equal(rayJob.Namespace, pg.Namespace)
	a.Equal("ray-rayjob-sample-pg", pg.Name)

	// Verify owner reference is set to RayJob
	a.Len(pg.OwnerReferences, 1)
	a.Equal("RayJob", pg.OwnerReferences[0].Kind)
	a.Equal(rayJob.Name, pg.OwnerReferences[0].Name)

	// Verify queue and priority class are set from RayJob labels
	a.Equal("test-queue", pg.Spec.Queue)
	a.Equal("high-priority", pg.Spec.PriorityClassName)

	// 1 head + 2 workers (desired, not min replicas)
	a.Equal(int32(3), pg.Spec.MinMember)
}

func TestAddMetadataToPod(t *testing.T) {
	a := assert.New(t)
	scheduler := &VolcanoBatchScheduler{}

	t.Run("RayCluster from RayJob", func(_ *testing.T) {
		cluster := createTestRayClusterFromRayJob("test-rayjob")
		cluster.Labels[QueueNameLabelKey] = "test-queue"
		cluster.Labels[utils.RayPriorityClassName] = "high-priority"

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      make(map[string]string),
				Annotations: make(map[string]string),
			},
			Spec: corev1.PodSpec{},
		}

		scheduler.AddMetadataToPod(context.Background(), &cluster, "worker", pod)

		// Should use RayJob name for pod group name
		a.Equal("ray-test-rayjob-pg", pod.Annotations[volcanoschedulingv1beta1.KubeGroupNameAnnotationKey])
		a.Equal("worker", pod.Annotations[volcanobatchv1alpha1.TaskSpecKey])
		a.Equal("volcano", pod.Spec.SchedulerName)
		a.Equal("test-queue", pod.Labels[QueueNameLabelKey])
		a.Equal("high-priority", pod.Labels[utils.RayPriorityClassName])
	})

	t.Run("Normal RayCluster", func(_ *testing.T) {
		cluster := createTestRayCluster(1)
		cluster.Labels = map[string]string{
			QueueNameLabelKey:          "test-queue",
			utils.RayPriorityClassName: "high-priority",
		}

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      make(map[string]string),
				Annotations: make(map[string]string),
			},
			Spec: corev1.PodSpec{},
		}

		scheduler.AddMetadataToPod(context.Background(), &cluster, "head", pod)

		a.Equal("ray-raycluster-sample-pg", pod.Annotations[volcanoschedulingv1beta1.KubeGroupNameAnnotationKey])
		a.Equal("head", pod.Annotations[volcanobatchv1alpha1.TaskSpecKey])
		a.Equal("volcano", pod.Spec.SchedulerName)
	})
}

func TestAddMetadataToSubmitterPod(t *testing.T) {
	a := assert.New(t)
	scheduler := &VolcanoBatchScheduler{}

	rayJob := createTestRayJob(1)

	job := &batchv1.Job{
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       corev1.PodSpec{},
			},
		},
	}

	scheduler.addMetadataToSubmitterPod(context.Background(), &rayJob, "submitter", job)

	// Check annotations
	a.Equal("ray-rayjob-sample-pg", job.Spec.Template.Annotations[volcanoschedulingv1beta1.KubeGroupNameAnnotationKey])
	a.Equal(utils.RayNodeSubmitterGroupLabelValue, job.Spec.Template.Annotations[volcanobatchv1alpha1.TaskSpecKey])

	// Check labels are copied from RayJob
	a.Equal("test-queue", job.Spec.Template.Labels[QueueNameLabelKey])
	a.Equal("high-priority", job.Spec.Template.Labels[utils.RayPriorityClassName])

	// Check scheduler name
	a.Equal("volcano", job.Spec.Template.Spec.SchedulerName)
}

func TestCalculatePodGroupParams(t *testing.T) {
	a := assert.New(t)
	scheduler := &VolcanoBatchScheduler{}

	t.Run("Autoscaling disabled", func(_ *testing.T) {
		cluster := createTestRayCluster(1)

		minMember, totalResource := scheduler.calculatePodGroupParams(context.Background(), &cluster.Spec)

		// 1 head + 2 workers (desired replicas)
		a.Equal(int32(3), minMember)

		// 256m * 3 (requests, not limits)
		a.Equal("768m", totalResource.Cpu().String())

		// 256Mi * 3 (requests, not limits)
		a.Equal("768Mi", totalResource.Memory().String())
	})

	t.Run("Autoscaling enabled", func(_ *testing.T) {
		cluster := createTestRayCluster(1)
		cluster.Spec.EnableInTreeAutoscaling = ptr.To(true)

		minMember, totalResource := scheduler.calculatePodGroupParams(context.Background(), &cluster.Spec)

		// 1 head + 1 worker (min replicas)
		a.Equal(int32(2), minMember)

		// 256m * 2 (requests, not limits)
		a.Equal("512m", totalResource.Cpu().String())

		// 256Mi * 2 (requests, not limits)
		a.Equal("512Mi", totalResource.Memory().String())
	})
}
