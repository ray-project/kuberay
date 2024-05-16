package expectations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	testPods []runtime.Object
)

func TestActiveExpectation_CreateAndDeletePod(t *testing.T) {
	setupTest()
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods[0]).Build()
	exp := NewActiveExpectations(fakeClient)
	rayClusterKey := "defaule/raycluster-test-pod"

	// Test expect create
	err := exp.ExpectCreate(rayClusterKey, Pod, testPods[1].(*corev1.Pod).Namespace, testPods[1].(*corev1.Pod).Name)
	assert.Nil(t, err, "Fail to set create expectation of pod2")
	satisfied, err := exp.IsSatisfied(rayClusterKey)
	assert.Nil(t, err, "Fail to check pod2")
	assert.Equal(t, satisfied, false)
	err = fakeClient.Create(context.TODO(), testPods[1].(*corev1.Pod))
	assert.Nil(t, err, "Fail to create pod2")
	satisfied, err = exp.IsSatisfied(rayClusterKey)
	assert.Nil(t, err, "Fail to check pod2")
	assert.Equal(t, satisfied, true)

	// Test expect delete
	err = exp.ExpectDelete(rayClusterKey, Pod, testPods[0].(*corev1.Pod).Namespace, testPods[0].(*corev1.Pod).Name)
	assert.Nil(t, err, "Fail to set delete expectation of pod1")
	err = exp.ExpectDelete(rayClusterKey, Pod, testPods[1].(*corev1.Pod).Namespace, testPods[1].(*corev1.Pod).Name)
	assert.Nil(t, err, "Fail to set delete expectation of pod2")
	satisfied, err = exp.IsSatisfied(rayClusterKey)
	assert.Nil(t, err, "Fail to check pod")
	assert.Equal(t, satisfied, false)

	// delete pod1
	err = fakeClient.Delete(context.TODO(), testPods[0].(*corev1.Pod))
	satisfied, err = exp.IsSatisfied(rayClusterKey)
	assert.Nil(t, err, "Fail to check pod")
	assert.Equal(t, satisfied, false)

	// delete pod2
	err = fakeClient.Delete(context.TODO(), testPods[1].(*corev1.Pod))
	satisfied, err = exp.IsSatisfied(rayClusterKey)
	assert.Nil(t, err, "Fail to check pod")
	assert.Equal(t, satisfied, true)
}

func TestActiveExpectation_DeleteAll(t *testing.T) {
	setupTest()
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods[0]).Build()
	exp := NewActiveExpectations(fakeClient)
	rayClusterKey := "defaule/raycluster-test"
	exp.ExpectCreate(rayClusterKey, Pod, testPods[1].(*corev1.Pod).Namespace, testPods[1].(*corev1.Pod).Name)
	satisfied, _ := exp.IsSatisfied(rayClusterKey)
	assert.Equal(t, satisfied, false)
	assert.Nil(t, exp.Delete(rayClusterKey), "Fail to delete all expectations")
	satisfied, _ = exp.IsSatisfied(rayClusterKey)
	assert.Equal(t, satisfied, true)
}

func setupTest() {
	testPods = []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "default",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: "default",
			},
		},
	}
}
