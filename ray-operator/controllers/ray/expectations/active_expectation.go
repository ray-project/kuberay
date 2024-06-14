package expectations

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

var ResourceInitializers map[ExpectedResourceType]func() client.Object

const ExpectationsTimeout = 10 * time.Minute

type ExpectedResourceType string

const (
	Pod        ExpectedResourceType = "Pod"
	RayCluster ExpectedResourceType = "RayCluster"
)

type ActiveExpectationAction int

const (
	Create ActiveExpectationAction = 0
	Delete ActiveExpectationAction = 1
)

func init() {
	ResourceInitializers = map[ExpectedResourceType]func() client.Object{
		Pod: func() client.Object {
			return &corev1.Pod{}
		},
		RayCluster: func() client.Object {
			return &rayv1.RayCluster{}
		},
	}
}

type ActiveExpectationsInterface interface {
	ExpectCreate(key string, kind ExpectedResourceType, namespace, name string) error
	ExpectDelete(key string, kind ExpectedResourceType, namespace, name string) error
	IsSatisfied(key string) bool
	DeleteItem(key string, kind ExpectedResourceType, name string) error
	Delete(key string) error
}

func NewActiveExpectations(client client.Client) ActiveExpectationsInterface {
	return &ActiveExpectations{
		Client:   client,
		subjects: cache.NewStore(ExpKeyFunc),
	}
}

type ActiveExpectations struct {
	client.Client
	subjects cache.Store
}

func (ae *ActiveExpectations) ExpectCreate(key string, kind ExpectedResourceType, namespace, name string) error {
	return ae.expectCreateOrDelete(key, kind, Create, namespace, name)
}

func (ae *ActiveExpectations) ExpectDelete(key string, kind ExpectedResourceType, namespace, name string) error {
	return ae.expectCreateOrDelete(key, kind, Delete, namespace, name)
}

func (ae *ActiveExpectations) expectCreateOrDelete(key string, kind ExpectedResourceType, action ActiveExpectationAction, namespace, name string) error {
	expectation, exist, err := ae.subjects.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to get expectation for active expectations %s when expecting: %w", key, err)
	}

	if !exist {
		expectation = NewActiveExpectation(ae.Client, key, namespace)
		if err = ae.subjects.Add(expectation); err != nil {
			return err
		}
	}

	if err = expectation.(*ActiveExpectation).expectCreateOrDelete(kind, name, action); err != nil {
		return fmt.Errorf("fail to expect %s/%s for action %d: %w", kind, name, action, err)
	}

	return nil
}

func (ae *ActiveExpectations) IsSatisfied(key string) (satisfied bool) {
	expectation, exist, _ := ae.subjects.GetByKey(key)
	if !exist {
		return true
	}
	defer func() {
		if satisfied {
			if err := ae.subjects.Delete(expectation); err != nil {
				panic(fmt.Errorf("fail to do delete expectation for active expectations %s when deleting: %w", key, err))
			}
		}
	}()
	satisfied = expectation.(*ActiveExpectation).isSatisfied()
	return
}

func (ae *ActiveExpectations) DeleteItem(key string, kind ExpectedResourceType, name string) error {
	if _, exist := ResourceInitializers[kind]; !exist {
		panic(fmt.Sprintf("kind %s is not supported for Active Expectation", kind))
	}

	expectation, exist, err := ae.subjects.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to get expectation for active expectations %s when deleting name %s: %w", key, name, err)
	}

	if !exist {
		return nil
	}

	item := expectation.(*ActiveExpectation)
	if err = item.delete(string(kind), name); err != nil {
		return fmt.Errorf("fail to delete %s/%s for key %s: %w", kind, name, key, err)
	}

	if len(item.items.List()) == 0 {
		if err = ae.subjects.Delete(expectation); err != nil {
			return fmt.Errorf("fail to do delete expectation for active expectations %s when deleting: %w", key, err)
		}
	}

	return nil
}

func (ae *ActiveExpectations) Delete(key string) error {
	expectation, exist, err := ae.subjects.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to get expectation for active expectations %s when deleting: %w", key, err)
	}

	if !exist {
		return nil
	}

	err = ae.subjects.Delete(expectation)
	if err != nil {
		return fmt.Errorf("fail to do delete expectation for active expectations %s when deleting: %w", key, err)
	}

	return nil
}

func ActiveExpectationItemKeyFunc(object interface{}) (string, error) {
	expectationItem, ok := object.(*ActiveExpectationItem)
	if !ok {
		return "", fmt.Errorf("fail to convert to active expectation item")
	}
	return expectationItem.Key, nil
}

func NewActiveExpectation(client client.Client, key, namespace string) *ActiveExpectation {
	return &ActiveExpectation{
		Client:          client,
		namespace:       namespace,
		key:             key,
		items:           cache.NewStore(ActiveExpectationItemKeyFunc),
		recordTimestamp: time.Now(),
	}
}

type ActiveExpectation struct {
	client.Client
	namespace       string
	key             string
	items           cache.Store
	recordTimestamp time.Time
}

func (ae *ActiveExpectation) expectCreateOrDelete(kind ExpectedResourceType, name string, action ActiveExpectationAction) error {
	key := fmt.Sprintf("%s/%s", kind, name)
	item, exist, err := ae.items.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to get active expectation item for %s when expecting: %w", key, err)
	}

	ae.recordTimestamp = time.Now()
	if !exist {
		item = &ActiveExpectationItem{Client: ae.Client, Name: name, Kind: kind, Key: key, Action: action, RecordTimestamp: time.Now()}
		return ae.items.Add(item)
	}

	item.(*ActiveExpectationItem).Action = action
	item.(*ActiveExpectationItem).RecordTimestamp = time.Now()
	return nil
}

func (ae *ActiveExpectation) isSatisfied() (satisfied bool) {
	items := ae.items.List()

	satisfied = true
	for _, item := range items {
		itemSatisfied := func() (satisfied bool) {
			defer func() {
				if satisfied {
					if err := ae.items.Delete(item); err != nil {
						panic(fmt.Errorf("fail to delete ActiveExpectation item %w", err))
					}
				} else if ae.recordTimestamp.Add(ExpectationsTimeout).Before(time.Now()) {
					panic("expected panic for active expectation")
				}
			}()
			satisfied = item.(*ActiveExpectationItem).isSatisfied(ae.namespace)
			return
		}()
		satisfied = satisfied && itemSatisfied
	}
	return satisfied
}

func (ae *ActiveExpectation) delete(kind, name string) error {
	key := fmt.Sprintf("%s/%s", kind, name)
	item, exist, err := ae.items.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to delete active expectation item for %s: %w", key, err)
	}

	if !exist {
		return nil
	}

	if err := ae.items.Delete(item); err != nil {
		return fmt.Errorf("fail to do delete active expectation item for %s: %w", key, err)
	}

	return nil
}

type ActiveExpectationItem struct {
	client.Client

	Key             string
	Name            string
	Kind            ExpectedResourceType
	Action          ActiveExpectationAction
	ResourceVersion int64
	RecordTimestamp time.Time
}

func (i *ActiveExpectationItem) isSatisfied(namespace string) bool {
	switch i.Action {
	case Create:
		resource := ResourceInitializers[i.Kind]()
		if err := i.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: i.Name}, resource); err == nil {
			return true
		} else {
			// tolerate watch event missing, after 30s
			return errors.IsNotFound(err) && i.RecordTimestamp.Add(30*time.Second).Before(time.Now())
		}
	case Delete:
		resource := ResourceInitializers[i.Kind]()
		if err := i.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: i.Name}, resource); err != nil {
			return errors.IsNotFound(err)
		} else {
			return resource.(metav1.Object).GetDeletionTimestamp() != nil
		}
	}
	return false
}

var ExpKeyFunc = func(obj interface{}) (string, error) {
	if e, ok := obj.(*ActiveExpectation); ok {
		return e.key, nil
	}
	panic(fmt.Sprintf("Could not find key for obj %#v", obj))
}
