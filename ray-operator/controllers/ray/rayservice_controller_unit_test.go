package ray

import (
	"testing"

	"github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

func TestGenerateRayClusterJsonHash(t *testing.T) {
	// `generateRayClusterJsonHash` will mute fields that will not trigger new RayCluster preparation. For example,
	// Autoscaler will update `Replicas` and `WorkersToDelete` when scaling up/down. Hence, `hash1` should be equal to
	// `hash2` in this case.
	cluster := v1alpha1.RayCluster{
		Spec: v1alpha1.RayClusterSpec{
			RayVersion: "2.4.0",
			WorkerGroupSpecs: []v1alpha1.WorkerGroupSpec{
				{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{},
					},
					Replicas:    pointer.Int32Ptr(2),
					MinReplicas: pointer.Int32Ptr(1),
					MaxReplicas: pointer.Int32Ptr(4),
				},
			},
		},
	}

	hash1, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)

	*cluster.Spec.WorkerGroupSpecs[0].Replicas++
	hash2, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)
	assert.Equal(t, hash1, hash2)

	// RayVersion will not be muted, so `hash3` should not be equal to `hash1`.
	cluster.Spec.RayVersion = "2.100.0"
	hash3, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)
	assert.NotEqual(t, hash1, hash3)
}

func TestCompareRayClusterJsonHash(t *testing.T) {
	cluster1 := v1alpha1.RayCluster{
		Spec: v1alpha1.RayClusterSpec{
			RayVersion: "2.4.0",
		},
	}
	cluster2 := cluster1.DeepCopy()
	cluster2.Spec.RayVersion = "2.100.0"
	equal, err := compareRayClusterJsonHash(cluster1.Spec, cluster2.Spec)
	assert.Nil(t, err)
	assert.False(t, equal)

	equal, err = compareRayClusterJsonHash(cluster1.Spec, cluster1.Spec)
	assert.Nil(t, err)
	assert.True(t, equal)
}
