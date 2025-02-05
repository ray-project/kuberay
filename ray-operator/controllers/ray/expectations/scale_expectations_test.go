package expectations

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRayClusterExpectationsHeadPod(t *testing.T) {
	ctx := context.Background()
	// Simulate local Informer with fakeClient.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects().Build()
	exp := NewRayClusterScaleExpectation(fakeClient)
	namespace := "default"
	rayClusterName := "raycluster-test"
	testPods := getTestPod()

	// Expect create head pod.
	exp.ExpectScalePod(namespace, rayClusterName, HeadGroup, testPods[0].Name, Create)
	// There is no head pod in Informer, return false.
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
	// Add a pod to the informer. This is used to simulate the informer syncing with the head pod in etcd.
	// In reality, it should be automatically done by the informer.
	err := fakeClient.Create(ctx, &testPods[0])
	require.NoError(t, err, "Fail to create head pod")
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
	// Expect delete head pod.
	exp.ExpectScalePod(namespace, rayClusterName, HeadGroup, testPods[0].Name, Delete)
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
	// Delete head pod from the informer.
	err = fakeClient.Delete(ctx, &testPods[0])
	require.NoError(t, err, "Fail to delete head pod")
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
}

func TestRayClusterExpectationsForSamePod(t *testing.T) {
	ctx := context.Background()
	// Simulate local Informer with fakeClient.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects().Build()
	exp := NewRayClusterScaleExpectation(fakeClient)
	namespace := "default"
	rayClusterName := "raycluster-test"
	testPods := getTestPod()

	// Expect the same Pod to be created and deleted.
	exp.ExpectScalePod(namespace, rayClusterName, HeadGroup, testPods[0].Name, Create)
	// Delete, override the expectation for the same Pod
	exp.ExpectScalePod(namespace, rayClusterName, HeadGroup, testPods[0].Name, Delete)
	// There is no pod in the informer. Satisfied. And delete expectation.
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
	err := fakeClient.Create(ctx, &testPods[0])
	require.NoError(t, err, "Fail to create head pod")
	// No expectation
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
	err = fakeClient.Delete(ctx, &testPods[0])
	require.NoError(t, err, "Fail to delete head pod")
	// No expectation
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
}

func TestRayClusterExpectationsWorkerGroupPods(t *testing.T) {
	ctx := context.Background()
	// Simulate local Informer with fakeClient.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects().Build()
	exp := NewRayClusterScaleExpectation(fakeClient)
	namespace := "default"
	rayClusterName := "raycluster-test"
	groupA := "test-group-a"
	groupB := "test-group-b"
	testPods := getTestPod()
	// Expect create one worker pod in group-a, two worker pods in group-b.
	exp.ExpectScalePod(namespace, rayClusterName, groupA, testPods[0].Name, Create)
	exp.ExpectScalePod(namespace, rayClusterName, groupB, testPods[1].Name, Create)
	exp.ExpectScalePod(namespace, rayClusterName, groupB, testPods[2].Name, Create)
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupA))
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupB))
	require.NoError(t, fakeClient.Create(ctx, &testPods[1]), "Fail to create worker pod2")
	// All pods within the same group are expected to meet.
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupB))
	require.NoError(t, fakeClient.Create(ctx, &testPods[2]), "Fail to create worker pod3")
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupB))
	// Different groups do not affect each other.
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupA))
	require.NoError(t, fakeClient.Create(ctx, &testPods[0]), "Fail to create worker pod1")
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupA))

	// Expect delete.
	exp.ExpectScalePod(namespace, rayClusterName, groupA, testPods[0].Name, Delete)
	exp.ExpectScalePod(namespace, rayClusterName, groupB, testPods[1].Name, Delete)
	exp.ExpectScalePod(namespace, rayClusterName, groupB, testPods[2].Name, Delete)
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupA))
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupB))
	require.NoError(t, fakeClient.Delete(ctx, &testPods[1]), "Fail to delete worker pod2")
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupB))
	require.NoError(t, fakeClient.Delete(ctx, &testPods[2]), "Fail to delete worker pod3")
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupB))
	// Different groups do not affect each other.
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupA))
	require.NoError(t, fakeClient.Delete(ctx, &testPods[0]), "Fail to delete worker pod1")
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, groupA))
}

func TestRayClusterExpectationsDeleteAll(t *testing.T) {
	ctx := context.Background()
	// Simulate local Informer with fakeClient.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects().Build()
	exp := NewRayClusterScaleExpectation(fakeClient)
	namespace := "default"
	rayClusterName := "raycluster-test"
	group := "test-group"
	testPods := getTestPod()
	exp.ExpectScalePod(namespace, rayClusterName, HeadGroup, testPods[0].Name, Create)
	exp.ExpectScalePod(namespace, rayClusterName, group, testPods[1].Name, Create)
	exp.ExpectScalePod(namespace, rayClusterName, group, testPods[2].Name, Delete)
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, group))
	// Delete all expectations
	exp.Delete(rayClusterName, namespace)
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, group))
}

func TestRayClusterExpectationsTimeout(t *testing.T) {
	ctx := context.Background()
	// Reduce the timeout duration so that tests don't have to wait for a long time.
	ExpectationsTimeout = 1 * time.Second
	// Simulate local Informer with fakeClient.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects().Build()
	exp := NewRayClusterScaleExpectation(fakeClient)
	namespace := "default"
	rayClusterName := "raycluster-test"
	testPods := getTestPod()

	exp.ExpectScalePod(namespace, rayClusterName, HeadGroup, testPods[0].Name, Create)
	assert.False(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
	// Expectations should be released after timeout.
	time.Sleep(ExpectationsTimeout + 1*time.Second)
	assert.True(t, exp.IsSatisfied(ctx, namespace, rayClusterName, HeadGroup))
}

func getTestPod() []corev1.Pod {
	return []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "default",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: "default",
			},
		},
	}
}
