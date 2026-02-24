package volcano

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	volcanobatchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	volcanoschedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
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
				utils.RayPriorityClassName: "test-priority",
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

func TestCreatePodGroupForRayCluster(t *testing.T) {
	a := assert.New(t)

	cluster := createTestRayCluster(1)

	minMember := utils.CalculateDesiredReplicas(&cluster) + 1
	totalResource := utils.CalculateDesiredResources(&cluster)
	pg, err := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)
	require.NoError(t, err)

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

func TestCreatePodGroupForRayCluster_NumOfHosts2(t *testing.T) {
	a := assert.New(t)

	cluster := createTestRayCluster(2)

	minMember := utils.CalculateDesiredReplicas(&cluster) + 1
	totalResource := utils.CalculateDesiredResources(&cluster)
	pg, err := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)
	require.NoError(t, err)

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

func createTestRayClusterWithLabels(labels map[string]string) rayv1.RayCluster {
	cluster := createTestRayCluster(1)
	if cluster.ObjectMeta.Labels == nil {
		cluster.ObjectMeta.Labels = make(map[string]string)
	}
	maps.Copy(cluster.ObjectMeta.Labels, labels)
	return cluster
}

func TestCreatePodGroup_NetworkTopologyBothLabels(t *testing.T) {
	a := assert.New(t)

	// Test with both network topology mode and highest tier allowed
	cluster := createTestRayClusterWithLabels(map[string]string{
		NetworkTopologyModeLabelKey:               "soft",
		NetworkTopologyHighestTierAllowedLabelKey: "3",
	})

	minMember := utils.CalculateDesiredReplicas(&cluster) + 1
	totalResource := utils.CalculateDesiredResources(&cluster)
	pg, err := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)
	require.NoError(t, err)

	a.Equal(cluster.Namespace, pg.Namespace)
	a.Equal(volcanoschedulingv1beta1.NetworkTopologyMode("soft"), pg.Spec.NetworkTopology.Mode)
	a.NotNil(pg.Spec.NetworkTopology.HighestTierAllowed)
	a.Equal(3, *pg.Spec.NetworkTopology.HighestTierAllowed)
}

func TestCreatePodGroup_NetworkTopologyOnlyModeLabel(t *testing.T) {
	a := assert.New(t)

	// Test with only network topology mode set
	cluster := createTestRayClusterWithLabels(map[string]string{
		NetworkTopologyModeLabelKey: "hard",
	})

	minMember := utils.CalculateDesiredReplicas(&cluster) + 1
	totalResource := utils.CalculateDesiredResources(&cluster)
	pg, err := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)
	require.NoError(t, err)

	a.Equal(cluster.Namespace, pg.Namespace)
	a.NotNil(pg.Spec.NetworkTopology)
	a.Equal(volcanoschedulingv1beta1.NetworkTopologyMode("hard"), pg.Spec.NetworkTopology.Mode)
	a.Nil(pg.Spec.NetworkTopology.HighestTierAllowed)
}

func TestCreatePodGroup_NetworkTopologyHighestTierAllowedNotInt(t *testing.T) {
	a := assert.New(t)

	// Test with network topology mode set and highest tier allowed is not an int
	cluster := createTestRayClusterWithLabels(map[string]string{
		NetworkTopologyModeLabelKey:               "soft",
		NetworkTopologyHighestTierAllowedLabelKey: "not-an-int",
	})

	minMember := utils.CalculateDesiredReplicas(&cluster) + 1
	totalResource := utils.CalculateDesiredResources(&cluster)
	pg, err := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)

	require.Error(t, err)
	a.Contains(err.Error(), "failed to convert "+NetworkTopologyHighestTierAllowedLabelKey+" label to int")
	a.Equal(cluster.Namespace, pg.Namespace)
}

func TestCreatePodGroupForRayJob(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	scheme := runtime.NewScheme()
	a.NoError(rayv1.AddToScheme(scheme))
	a.NoError(volcanoschedulingv1beta1.AddToScheme(scheme))
	fakeCli := fake.NewClientBuilder().WithScheme(scheme).Build()
	scheduler := &VolcanoBatchScheduler{cli: fakeCli}

	t.Run("No submitter pod resources", func(_ *testing.T) {
		rayJob := createTestRayJob(1)
		rayJob.Spec.SubmissionMode = rayv1.HTTPMode

		err := scheduler.handleRayJob(ctx, &rayJob)
		require.NoError(t, err)

		var pg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: getAppPodGroupName(&rayJob)}, &pg)
		require.NoError(t, err)

		// 1 head + 2 workers (desired, not min replicas)
		a.Equal(int32(3), pg.Spec.MinMember)
		// 256m * 3 (requests, not limits)
		a.Equal("768m", pg.Spec.MinResources.Cpu().String())
		// 256m * 3 (requests, not limits)
		a.Equal("768Mi", pg.Spec.MinResources.Memory().String())
		a.Equal("test-queue", pg.Spec.Queue)
		a.Equal("test-priority", pg.Spec.PriorityClassName)
		a.Len(pg.OwnerReferences, 1)
		a.Equal("RayJob", pg.OwnerReferences[0].Kind)
	})

	t.Run("K8sJobMode includes submitter pod resources", func(_ *testing.T) {
		rayJob := createTestRayJob(1)
		rayJob.Spec.SubmissionMode = rayv1.K8sJobMode

		err := scheduler.handleRayJob(ctx, &rayJob)
		require.NoError(t, err)

		var pg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: getAppPodGroupName(&rayJob)}, &pg)
		require.NoError(t, err)

		// 1 head + 2 workers (desired, not min replicas)
		a.Equal(int32(3), pg.Spec.MinMember)
		// 768m + 500m = 1268m
		a.Equal("1268m", pg.Spec.MinResources.Cpu().String())
		// 768Mi + 200Mi = 968Mi
		a.Equal("968Mi", pg.Spec.MinResources.Memory().String())
		a.Equal("test-queue", pg.Spec.Queue)
		a.Equal("test-priority", pg.Spec.PriorityClassName)
		a.Len(pg.OwnerReferences, 1)
		a.Equal("RayJob", pg.OwnerReferences[0].Kind)
	})

	t.Run("SidecarMode includes submitter pod resources", func(_ *testing.T) {
		rayJob := createTestRayJob(1)
		rayJob.Spec.SubmissionMode = rayv1.SidecarMode

		err := scheduler.handleRayJob(ctx, &rayJob)
		require.NoError(t, err)

		var pg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: getAppPodGroupName(&rayJob)}, &pg)
		require.NoError(t, err)

		// 1 head + 2 workers (desired, not min replicas)
		a.Equal(int32(3), pg.Spec.MinMember)
		// 768m + 500m = 1268m
		a.Equal("1268m", pg.Spec.MinResources.Cpu().String())
		// 768Mi + 200Mi = 968Mi
		a.Equal("968Mi", pg.Spec.MinResources.Memory().String())
		a.Equal("test-queue", pg.Spec.Queue)
		a.Equal("test-priority", pg.Spec.PriorityClassName)
		a.Len(pg.OwnerReferences, 1)
		a.Equal("RayJob", pg.OwnerReferences[0].Kind)
	})
}

func TestCreatePodGroupForRayJob_NumOfHosts2(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	scheme := runtime.NewScheme()
	a.NoError(rayv1.AddToScheme(scheme))
	a.NoError(volcanoschedulingv1beta1.AddToScheme(scheme))
	fakeCli := fake.NewClientBuilder().WithScheme(scheme).Build()
	scheduler := &VolcanoBatchScheduler{cli: fakeCli}

	t.Run("No submitter pod resources", func(_ *testing.T) {
		rayJob := createTestRayJob(2)
		rayJob.Spec.SubmissionMode = rayv1.HTTPMode

		err := scheduler.handleRayJob(ctx, &rayJob)
		require.NoError(t, err)

		var pg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: getAppPodGroupName(&rayJob)}, &pg)
		require.NoError(t, err)

		// 2 workers (desired, not min replicas) * 2 (num of hosts) + 1 head
		// 2 * 2 + 1 = 5
		a.Equal(int32(5), pg.Spec.MinMember)
		// 256m * (2 (requests, not limits) * 2 (num of hosts) + 1 head)
		// 256m * 5 = 1280m
		a.Equal("1280m", pg.Spec.MinResources.Cpu().String())
		// 256Mi * (2 (requests, not limits) * 2 (num of hosts) + 1 head)
		// 256Mi * 5 = 1280Mi
		a.Equal("1280Mi", pg.Spec.MinResources.Memory().String())
		a.Equal("test-queue", pg.Spec.Queue)
		a.Equal("test-priority", pg.Spec.PriorityClassName)
		a.Len(pg.OwnerReferences, 1)
		a.Equal("RayJob", pg.OwnerReferences[0].Kind)
	})

	t.Run("K8sJobMode includes submitter pod resources", func(_ *testing.T) {
		rayJob := createTestRayJob(2)
		rayJob.Spec.SubmissionMode = rayv1.K8sJobMode

		err := scheduler.handleRayJob(ctx, &rayJob)
		require.NoError(t, err)

		var pg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: getAppPodGroupName(&rayJob)}, &pg)
		require.NoError(t, err)

		// 2 workers (desired, not min replicas) * 2 (num of hosts) + 1 head
		// 2 * 2 + 1 = 5
		a.Equal(int32(5), pg.Spec.MinMember)
		// 1280m + 500m = 1780m
		a.Equal("1780m", pg.Spec.MinResources.Cpu().String())
		// 1280Mi + 200Mi = 1480Mi
		a.Equal("1480Mi", pg.Spec.MinResources.Memory().String())
		a.Equal("test-queue", pg.Spec.Queue)
		a.Equal("test-priority", pg.Spec.PriorityClassName)
		a.Len(pg.OwnerReferences, 1)
		a.Equal("RayJob", pg.OwnerReferences[0].Kind)
	})

	t.Run("SidecarMode includes submitter pod resources", func(_ *testing.T) {
		rayJob := createTestRayJob(2)
		rayJob.Spec.SubmissionMode = rayv1.SidecarMode

		err := scheduler.handleRayJob(ctx, &rayJob)
		require.NoError(t, err)

		var pg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: getAppPodGroupName(&rayJob)}, &pg)
		require.NoError(t, err)

		// 2 workers (desired, not min replicas) * 2 (num of hosts) + 1 head
		// 2 * 2 + 1 = 5
		a.Equal(int32(5), pg.Spec.MinMember)
		// 1280m + 500m = 1780m
		a.Equal("1780m", pg.Spec.MinResources.Cpu().String())
		// 1280Mi + 200Mi = 1480Mi
		a.Equal("1480Mi", pg.Spec.MinResources.Memory().String())
		a.Equal("test-queue", pg.Spec.Queue)
		a.Equal("test-priority", pg.Spec.PriorityClassName)
		a.Len(pg.OwnerReferences, 1)
		a.Equal("RayJob", pg.OwnerReferences[0].Kind)
	})
}

func TestAddMetadataToSubmitterPod(t *testing.T) {
	a := assert.New(t)
	scheduler := &VolcanoBatchScheduler{}

	rayJob := createTestRayJob(1)
	rayCluster := &rayv1.RayCluster{Spec: *rayJob.Spec.RayClusterSpec}
	submitterTemplate := common.GetSubmitterTemplate(&rayJob.Spec, &rayCluster.Spec)

	scheduler.AddMetadataToChildResource(
		context.Background(),
		&rayJob,
		&submitterTemplate,
		utils.RayNodeSubmitterGroupLabelValue,
	)

	// Check annotations
	a.Equal(getAppPodGroupName(&rayJob), submitterTemplate.Annotations[volcanoschedulingv1beta1.KubeGroupNameAnnotationKey])
	a.Equal(utils.RayNodeSubmitterGroupLabelValue, submitterTemplate.Annotations[volcanobatchv1alpha1.TaskSpecKey])

	// Check labels
	a.Equal("test-queue", submitterTemplate.Labels[QueueNameLabelKey])
	a.Equal("test-priority", submitterTemplate.Labels[utils.RayPriorityClassName])

	// Check scheduler name
	a.Equal(pluginName, submitterTemplate.Spec.SchedulerName)
}

func TestCalculatePodGroupParams(t *testing.T) {
	a := assert.New(t)
	scheduler := &VolcanoBatchScheduler{}

	t.Run("Autoscaling disabled", func(_ *testing.T) {
		cluster := createTestRayCluster(1)

		minMember, totalResource := scheduler.calculatePodGroupParams(&cluster.Spec)

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

		minMember, totalResource := scheduler.calculatePodGroupParams(&cluster.Spec)

		// 1 head + 1 worker (min replicas)
		a.Equal(int32(2), minMember)

		// 256m * 2 (requests, not limits)
		a.Equal("512m", totalResource.Cpu().String())

		// 256Mi * 2 (requests, not limits)
		a.Equal("512Mi", totalResource.Memory().String())
	})
}

func TestGetAppPodGroupName(t *testing.T) {
	a := assert.New(t)

	rayCluster := &rayv1.RayCluster{ObjectMeta: metav1.ObjectMeta{Name: "raycluster-sample", Namespace: "default"}}
	a.Equal("ray-raycluster-sample-pg", getAppPodGroupName(rayCluster))

	rayJob := createTestRayJob(1)
	a.Equal("ray-rayjob-sample-pg", getAppPodGroupName(&rayJob))
}

func TestCreatePodGroup_OwnerAnnotationsCopied(t *testing.T) {
	a := assert.New(t)

	t.Run("RayCluster with annotations", func(t *testing.T) {
		cluster := createTestRayCluster(1)
		cluster.Annotations = map[string]string{
			"custom.io/team":       "ml-platform",
			"custom.io/scheduling": "volcano",
		}

		minMember := utils.CalculateDesiredReplicas(&cluster) + 1
		totalResource := utils.CalculateDesiredResources(&cluster)
		pg, err := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)
		require.NoError(t, err)

		a.NotNil(pg.Annotations)
		a.Equal(cluster.Annotations["custom.io/team"], pg.Annotations["custom.io/team"])
		a.Equal(cluster.Annotations["custom.io/scheduling"], pg.Annotations["custom.io/scheduling"])
		a.Len(pg.Annotations, 2)
	})

	t.Run("RayJob with annotations", func(t *testing.T) {
		rayJob := createTestRayJob(1)
		rayJob.Annotations = map[string]string{
			"job-type": "training",
			"owner":    "data-team",
		}

		minMember := utils.CalculateDesiredReplicas(&rayv1.RayCluster{Spec: *rayJob.Spec.RayClusterSpec}) + 1
		totalResource := utils.CalculateDesiredResources(&rayv1.RayCluster{Spec: *rayJob.Spec.RayClusterSpec})
		pg, err := createPodGroup(&rayJob, getAppPodGroupName(&rayJob), minMember, totalResource)
		require.NoError(t, err)

		a.NotNil(pg.Annotations)
		a.Equal(rayJob.Annotations["job-type"], pg.Annotations["job-type"])
		a.Equal(rayJob.Annotations["owner"], pg.Annotations["owner"])
		a.Len(pg.Annotations, 2)
	})

	t.Run("RayCluster with nil annotations", func(t *testing.T) {
		cluster := createTestRayCluster(1)
		cluster.Annotations = nil

		minMember := utils.CalculateDesiredReplicas(&cluster) + 1
		totalResource := utils.CalculateDesiredResources(&cluster)
		pg, err := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)
		require.NoError(t, err)

		a.NotNil(pg.Annotations)
		a.Empty(pg.Annotations)
	})

	t.Run("RayCluster with empty annotations", func(t *testing.T) {
		cluster := createTestRayCluster(1)
		cluster.Annotations = map[string]string{}

		minMember := utils.CalculateDesiredReplicas(&cluster) + 1
		totalResource := utils.CalculateDesiredResources(&cluster)
		pg, err := createPodGroup(&cluster, getAppPodGroupName(&cluster), minMember, totalResource)
		require.NoError(t, err)

		a.NotNil(pg.Annotations)
		a.Empty(pg.Annotations)
	})
}

func TestCleanupOnCompletion(t *testing.T) {
	a := assert.New(t)
	require := require.New(t)

	t.Run("RayJob - delete PodGroup", func(_ *testing.T) {
		rayJob := createTestRayJob(1)
		scheme := runtime.NewScheme()
		a.NoError(rayv1.AddToScheme(scheme))
		a.NoError(volcanoschedulingv1beta1.AddToScheme(scheme))
		fakeCli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&volcanoschedulingv1beta1.PodGroup{}).Build()
		scheduler := &VolcanoBatchScheduler{cli: fakeCli}

		ctx := context.Background()

		// Manually create a PodGroup in Pending state to simulate the real scenario
		podGroupName := getAppPodGroupName(&rayJob)
		pg := &volcanoschedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podGroupName,
				Namespace: rayJob.Namespace,
			},
			Spec: volcanoschedulingv1beta1.PodGroupSpec{
				MinMember: 3,
			},
			Status: volcanoschedulingv1beta1.PodGroupStatus{
				Phase:   volcanoschedulingv1beta1.PodGroupPending,
				Running: 1,
			},
		}
		err := fakeCli.Create(ctx, pg)
		require.NoError(err)

		// Verify PodGroup was created in Pending state
		var retrievedPg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: podGroupName}, &retrievedPg)
		require.NoError(err)
		a.Equal(volcanoschedulingv1beta1.PodGroupPending, retrievedPg.Status.Phase)

		// Now call CleanupOnCompletion to simulate RayJob finishing
		err = scheduler.CleanupOnCompletion(ctx, &rayJob)
		require.NoError(err)

		// Verify PodGroup was deleted
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: podGroupName}, &retrievedPg)
		require.Error(err)
		a.True(errors.IsNotFound(err))
	})

	t.Run("RayJob - PodGroup not found (already deleted)", func(_ *testing.T) {
		rayJob := createTestRayJob(1)
		scheme := runtime.NewScheme()
		a.NoError(rayv1.AddToScheme(scheme))
		a.NoError(volcanoschedulingv1beta1.AddToScheme(scheme))
		fakeCli := fake.NewClientBuilder().WithScheme(scheme).Build()
		scheduler := &VolcanoBatchScheduler{cli: fakeCli}

		ctx := context.Background()

		// Don't create a PodGroup, just call CleanupOnCompletion
		err := scheduler.CleanupOnCompletion(ctx, &rayJob)
		// Should not return an error, just log that PodGroup was not found
		require.NoError(err)
	})

	t.Run("RayCluster - should be no-op", func(_ *testing.T) {
		rayCluster := createTestRayCluster(1)
		scheme := runtime.NewScheme()
		a.NoError(rayv1.AddToScheme(scheme))
		a.NoError(volcanoschedulingv1beta1.AddToScheme(scheme))
		fakeCli := fake.NewClientBuilder().WithScheme(scheme).Build()
		scheduler := &VolcanoBatchScheduler{cli: fakeCli}

		ctx := context.Background()

		// Call CleanupOnCompletion with RayCluster - should be no-op
		err := scheduler.CleanupOnCompletion(ctx, &rayCluster)
		require.NoError(err)

		// Verify no PodGroup was created (RayCluster PodGroups are not managed by this method)
		var pg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayCluster.Namespace, Name: getAppPodGroupName(&rayCluster)}, &pg)
		require.Error(err) // Should not be found
		a.True(errors.IsNotFound(err))
	})

	t.Run("RayJob - PodGroup in Inqueue state (bug scenario)", func(_ *testing.T) {
		rayJob := createTestRayJob(1)
		scheme := runtime.NewScheme()
		a.NoError(rayv1.AddToScheme(scheme))
		a.NoError(volcanoschedulingv1beta1.AddToScheme(scheme))
		fakeCli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&volcanoschedulingv1beta1.PodGroup{}).Build()
		scheduler := &VolcanoBatchScheduler{cli: fakeCli}

		ctx := context.Background()

		// Create a PodGroup in Inqueue state to simulate the bug scenario
		podGroupName := getAppPodGroupName(&rayJob)
		pg := &volcanoschedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podGroupName,
				Namespace: rayJob.Namespace,
			},
			Spec: volcanoschedulingv1beta1.PodGroupSpec{
				MinMember: 31,
			},
			Status: volcanoschedulingv1beta1.PodGroupStatus{
				Phase:     volcanoschedulingv1beta1.PodGroupInqueue,
				Running:   0,
				Succeeded: 1,
			},
		}
		err := fakeCli.Create(ctx, pg)
		require.NoError(err)

		// Call CleanupOnCompletion
		err = scheduler.CleanupOnCompletion(ctx, &rayJob)
		require.NoError(err)

		// Verify PodGroup was deleted
		var retrievedPg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: podGroupName}, &retrievedPg)
		require.Error(err)
		a.True(errors.IsNotFound(err))
	})

	t.Run("RayJob - idempotent (can call multiple times)", func(_ *testing.T) {
		rayJob := createTestRayJob(1)
		scheme := runtime.NewScheme()
		a.NoError(rayv1.AddToScheme(scheme))
		a.NoError(volcanoschedulingv1beta1.AddToScheme(scheme))
		fakeCli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&volcanoschedulingv1beta1.PodGroup{}).Build()
		scheduler := &VolcanoBatchScheduler{cli: fakeCli}

		ctx := context.Background()

		// Create a PodGroup
		podGroupName := getAppPodGroupName(&rayJob)
		pg := &volcanoschedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podGroupName,
				Namespace: rayJob.Namespace,
			},
			Spec: volcanoschedulingv1beta1.PodGroupSpec{
				MinMember: 3,
			},
			Status: volcanoschedulingv1beta1.PodGroupStatus{
				Phase:   volcanoschedulingv1beta1.PodGroupPending,
				Running: 1,
			},
		}
		err := fakeCli.Create(ctx, pg)
		require.NoError(err)

		// Call CleanupOnCompletion first time
		err = scheduler.CleanupOnCompletion(ctx, &rayJob)
		require.NoError(err)

		// Verify PodGroup was deleted
		var retrievedPg volcanoschedulingv1beta1.PodGroup
		err = fakeCli.Get(ctx, client.ObjectKey{Namespace: rayJob.Namespace, Name: podGroupName}, &retrievedPg)
		require.Error(err)
		a.True(errors.IsNotFound(err))

		// Call CleanupOnCompletion second time - should not error
		err = scheduler.CleanupOnCompletion(ctx, &rayJob)
		require.NoError(err)
	})
}
