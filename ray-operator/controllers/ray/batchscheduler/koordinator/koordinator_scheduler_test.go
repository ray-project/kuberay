package koordinator

import (
	"context"
	"testing"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/stretchr/testify/assert"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createRayClusterWithLabels(namespace, name string, labels map[string]string) *rayv1.RayCluster {
	rayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
	}

	return rayCluster
}

func setHeadPodNamespace(app *rayv1.RayCluster, namespace string) {
	app.Spec.HeadGroupSpec.Template.Namespace = namespace
}

func addWorkerPodSpec(app *rayv1.RayCluster, workerGroupName string,
	namespace string, replicas, minReplicas int32) {
	workerGroupSpec := rayv1.WorkerGroupSpec{
		GroupName:   workerGroupName,
		Replicas:    &replicas,
		MinReplicas: &minReplicas,
	}
	workerGroupSpec.Template.Namespace = namespace

	app.Spec.WorkerGroupSpecs = append(app.Spec.WorkerGroupSpecs, workerGroupSpec)
}

func createPod(namespace, name string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
		},
	}
}

func TestAddMetadataToPod(t *testing.T) {
	ks := &KoodinatorScheduler{}
	ctx := context.Background()
	// test the case when gang-scheduling is enabled
	rayClusterWithGangScheduling := createRayClusterWithLabels(
		"ray-namespace",
		"koord",
		map[string]string{
			utils.RayClusterGangSchedulingEnabled: "true",
		},
	)

	setHeadPodNamespace(rayClusterWithGangScheduling, "ns0")
	addWorkerPodSpec(rayClusterWithGangScheduling, "workergroup1", "ns1", 4, 2)
	addWorkerPodSpec(rayClusterWithGangScheduling, "workergroup2", "ns2", 5, 3)

	gangGroupValue := `["ns0/ray-koord-headgroup","ns1/ray-koord-workergroup1","ns2/ray-koord-workergroup2"]`

	// case 1: head pod
	headPod := createPod("ns0", "head-pod")
	ks.AddMetadataToPod(context.Background(), rayClusterWithGangScheduling,
		utils.RayNodeHeadGroupLabelValue, headPod)
	// verify the correctness of head pod
	assert.Equal(t, "ray-koord-headgroup", headPod.Annotations[KoordinatorGangAnnotationName])
	assert.Equal(t, "1", headPod.Annotations[KoordinatorGangMinAvailableAnnotationName])
	assert.Equal(t, "1", headPod.Annotations[KoordinatorGangTotalNumberAnnotationName])
	assert.Equal(t, KoordinatorGangModeStrict, headPod.Annotations[KoordinatorGangModeAnnotationName])
	assert.Equal(t, gangGroupValue, headPod.Annotations[KoordinatorGangGroupsAnnotationName])

	// case2: woker pod 1
	workerpod1 := createPod("ns1", "workerpod1")
	ks.AddMetadataToPod(ctx, rayClusterWithGangScheduling, "workergroup1", workerpod1)
	// verify the correctness of woker pod 1
	assert.Equal(t, "ray-koord-workergroup1", workerpod1.Annotations[KoordinatorGangAnnotationName])
	assert.Equal(t, "2", workerpod1.Annotations[KoordinatorGangMinAvailableAnnotationName])
	assert.Equal(t, "4", workerpod1.Annotations[KoordinatorGangTotalNumberAnnotationName])
	assert.Equal(t, KoordinatorGangModeStrict, workerpod1.Annotations[KoordinatorGangModeAnnotationName])
	assert.Equal(t, gangGroupValue, workerpod1.Annotations[KoordinatorGangGroupsAnnotationName])

	// case3: woker pod 2
	workerpod2 := createPod("ns2", "workerpod2")
	ks.AddMetadataToPod(ctx, rayClusterWithGangScheduling, "workergroup2", workerpod2)
	// verify the correctness of woker pod 2
	assert.Equal(t, "ray-koord-workergroup2", workerpod2.Annotations[KoordinatorGangAnnotationName])
	assert.Equal(t, "3", workerpod2.Annotations[KoordinatorGangMinAvailableAnnotationName])
	assert.Equal(t, "5", workerpod2.Annotations[KoordinatorGangTotalNumberAnnotationName])
	assert.Equal(t, KoordinatorGangModeStrict, workerpod2.Annotations[KoordinatorGangModeAnnotationName])
	assert.Equal(t, gangGroupValue, workerpod2.Annotations[KoordinatorGangGroupsAnnotationName])

}
