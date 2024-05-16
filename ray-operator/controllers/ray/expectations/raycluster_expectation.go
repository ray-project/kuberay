package expectations

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

const defaultHead = ""

func RayClusterKey(cluster *rayv1.RayCluster) string {
	return fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
}

func RayClusterHeadKey(rayClusterKey string) string {
	return fmt.Sprintf("%s/head", rayClusterKey)
}

func RayClusterGroupKey(rayClusterKey, group string) string {
	return fmt.Sprintf("%s/worker/%s", rayClusterKey, group)
}

func NewRayClusterExpectations(client client.Client) *RayClusterExpectations {
	return &RayClusterExpectations{
		groupStore: make(map[string]sets.Set[string]),
		exp:        NewActiveExpectations(client),
	}
}

type RayClusterExpectations struct {
	mu         sync.RWMutex
	groupStore map[string]sets.Set[string]
	exp        ActiveExpectationsInterface
}

func (rc *RayClusterExpectations) record(rayClusterKey, group string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	groups, ok := rc.groupStore[rayClusterKey]
	if !ok {
		groups = sets.New[string](group)
		rc.groupStore[rayClusterKey] = groups
		return
	}
	if !groups.Has(group) {
		groups.Insert(group)
	}
}

func (rc *RayClusterExpectations) ExpectCreateHeadPod(rayClusterKey, namespace, name string) {
	headKey := RayClusterHeadKey(rayClusterKey)
	rc.record(rayClusterKey, defaultHead)
	_ = rc.exp.ExpectCreate(headKey, Pod, namespace, name)
}

func (rc *RayClusterExpectations) ExpectCreateWorkerPod(rayClusterKey, group, namespace, name string) {
	groupKey := RayClusterGroupKey(rayClusterKey, group)
	rc.record(rayClusterKey, group)
	_ = rc.exp.ExpectCreate(groupKey, Pod, namespace, name)
}

func (rc *RayClusterExpectations) ExpectDeleteHeadPod(rayClusterKey, namespace, name string) {
	headKey := RayClusterHeadKey(rayClusterKey)
	rc.record(rayClusterKey, defaultHead)
	_ = rc.exp.ExpectDelete(headKey, Pod, namespace, name)
}

func (rc *RayClusterExpectations) ExpectDeleteWorkerPod(rayClusterKey, group, namespace, name string) {
	groupKey := RayClusterGroupKey(rayClusterKey, group)
	rc.record(rayClusterKey, group)
	_ = rc.exp.ExpectDelete(groupKey, Pod, namespace, name)
}

func (rc *RayClusterExpectations) IsHeadSatisfied(rayClusterKey string) bool {
	ok, _ := rc.exp.IsSatisfied(RayClusterHeadKey(rayClusterKey))
	return ok
}

func (rc *RayClusterExpectations) IsGroupSatisfied(rayClusterKey, group string) bool {
	ok, _ := rc.exp.IsSatisfied(RayClusterGroupKey(rayClusterKey, group))
	return ok
}

func (rc *RayClusterExpectations) Delete(rayClusterKey string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	groups, ok := rc.groupStore[rayClusterKey]
	if !ok {
		return
	}
	for group := range groups {
		if group == defaultHead {
			_ = rc.exp.Delete(RayClusterHeadKey(rayClusterKey))
		} else {
			_ = rc.exp.Delete(RayClusterGroupKey(rayClusterKey, group))
		}
	}
	delete(rc.groupStore, rayClusterKey)
}
