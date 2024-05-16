package expectations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRayClusterExpectations(t *testing.T) {
	setupTest()
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects().Build()
	exp := NewRayClusterExpectations(fakeClient)
	rayClusterKey := "defaule/raycluster-test-pod"

	// Test expect create head
	exp.ExpectCreateHeadPod(rayClusterKey, testPods[0].(*corev1.Pod).Namespace, testPods[0].(*corev1.Pod).Name)
	assert.Equal(t, exp.IsHeadSatisfied(rayClusterKey), false)
	err := fakeClient.Create(context.TODO(), testPods[0].(*corev1.Pod))
	assert.Nil(t, err, "Fail to create pod1")
	assert.Equal(t, exp.IsHeadSatisfied(rayClusterKey), true)

	// Test expect delete head
	exp.ExpectDeleteHeadPod(rayClusterKey, testPods[0].(*corev1.Pod).Namespace, testPods[0].(*corev1.Pod).Name)
	assert.Equal(t, exp.IsHeadSatisfied(rayClusterKey), false)
	// delete pod2
	err = fakeClient.Delete(context.TODO(), testPods[0].(*corev1.Pod))
	assert.Equal(t, exp.IsHeadSatisfied(rayClusterKey), true)

	// Test expect create worker
	group := "test-group"
	exp.ExpectCreateWorkerPod(rayClusterKey, group, testPods[1].(*corev1.Pod).Namespace, testPods[1].(*corev1.Pod).Name)
	exp.ExpectCreateWorkerPod(rayClusterKey, group, testPods[2].(*corev1.Pod).Namespace, testPods[2].(*corev1.Pod).Name)
	assert.Equal(t, exp.IsGroupSatisfied(rayClusterKey, group), false)
	assert.Nil(t, fakeClient.Create(context.TODO(), testPods[1].(*corev1.Pod)), "Fail to create pod2")
	assert.Equal(t, exp.IsGroupSatisfied(rayClusterKey, group), false)
	assert.Nil(t, fakeClient.Create(context.TODO(), testPods[2].(*corev1.Pod)), "Fail to create pod3")
	assert.Equal(t, exp.IsGroupSatisfied(rayClusterKey, group), true)

	// Test delete all
	// reset pods
	setupTest()
	exp.ExpectCreateHeadPod(rayClusterKey, testPods[0].(*corev1.Pod).Namespace, testPods[0].(*corev1.Pod).Name)
	exp.ExpectDeleteWorkerPod(rayClusterKey, group, testPods[1].(*corev1.Pod).Namespace, testPods[1].(*corev1.Pod).Name)
	exp.ExpectDeleteWorkerPod(rayClusterKey, group, testPods[2].(*corev1.Pod).Namespace, testPods[2].(*corev1.Pod).Name)
	assert.Equal(t, exp.IsGroupSatisfied(rayClusterKey, group), false)
	assert.Equal(t, exp.IsHeadSatisfied(rayClusterKey), false)
	exp.Delete(rayClusterKey)
	assert.Equal(t, exp.IsGroupSatisfied(rayClusterKey, group), true)
	assert.Equal(t, exp.IsHeadSatisfied(rayClusterKey), true)
}
