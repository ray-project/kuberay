package expectations

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

var (
	ResourceInitializers map[ExpectedResourceType]func() client.Object
)

const (
	ExpectationsTimeout = 10 * time.Minute
)

type ExpectedResourceType string

const (
	Pod        ExpectedResourceType = "Pod"
	RayCluster ExpectedResourceType = "RayCluster"
)

type ActiveExpectationAction int

const (
	Create ActiveExpectationAction = 0
	Delete ActiveExpectationAction = 1
	Update ActiveExpectationAction = 3
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
	ExpectUpdate(key string, kind ExpectedResourceType, namespace, name string, updatedResourceVersion string) error
	IsSatisfied(key string) (satisfied bool, err error)
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
		return fmt.Errorf("fail to get expectation for active expectations %s when expecting: %s", key, err)
	}

	if !exist {
		expectation = NewActiveExpectation(ae.Client, key, namespace)
		if err = ae.subjects.Add(expectation); err != nil {
			return err
		}
	}

	if err = expectation.(*ActiveExpectation).expectCreateOrDelete(kind, name, action); err != nil {
		return fmt.Errorf("fail to expect %s/%s for action %d: %s", kind, name, action, err)
	}

	return nil
}

func (ae *ActiveExpectations) ExpectUpdate(key string, kind ExpectedResourceType, namespace, name string, updatedResourceVersion string) error {
	rv, err := strconv.ParseInt(updatedResourceVersion, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("fail to parse resource version %s of resource %s/%s to int64 for subject %s: %s",
			updatedResourceVersion, kind, name, key, err))
	}

	if _, exist := ResourceInitializers[kind]; !exist {
		panic(fmt.Sprintf("kind %s is not supported for Active Expectation", kind))
	}

	expectation, exist, err := ae.subjects.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to get expectation for active expectations %s when expecting: %s", key, err)
	}

	if !exist {
		expectation = NewActiveExpectation(ae.Client, namespace, key)
		if err := ae.subjects.Add(expectation); err != nil {
			return err
		}
	}

	if err := expectation.(*ActiveExpectation).expectUpdate(kind, name, rv); err != nil {
		return fmt.Errorf("fail to expect %s/%s for action %d, %s: %s", kind, name, Update, updatedResourceVersion, err)
	}

	return nil
}

func (ae *ActiveExpectations) IsSatisfied(key string) (satisfied bool, err error) {
	expectation, exist, err := ae.subjects.GetByKey(key)
	if err != nil {
		return false, fmt.Errorf("fail to get expectation for active expectations %s when check satisfication: %s", key, err)
	}

	if !exist {
		return true, nil
	}

	defer func() {
		if satisfied {
			ae.subjects.Delete(expectation)
		}
	}()

	satisfied, err = expectation.(*ActiveExpectation).isSatisfied()
	if err != nil {
		return false, err
	}

	return
}

func (ae *ActiveExpectations) DeleteItem(key string, kind ExpectedResourceType, name string) error {
	if _, exist := ResourceInitializers[kind]; !exist {
		panic(fmt.Sprintf("kind %s is not supported for Active Expectation", kind))
	}

	expectation, exist, err := ae.subjects.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to get expectation for active expectations %s when deleting name %s: %s", key, name, err)
	}

	if !exist {
		return nil
	}

	item := expectation.(*ActiveExpectation)
	if err := item.delete(string(kind), name); err != nil {
		return fmt.Errorf("fail to delete %s/%s for key %s: %s", kind, name, key, err)
	}

	if len(item.items.List()) == 0 {
		ae.subjects.Delete(expectation)
	}

	return nil
}

func (ae *ActiveExpectations) Delete(key string) error {
	expectation, exist, err := ae.subjects.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to get expectation for active expectations %s when deleting: %s", key, err)
	}

	if !exist {
		return nil
	}

	err = ae.subjects.Delete(expectation)
	if err != nil {
		return fmt.Errorf("fail to do delete expectation for active expectations %s when deleting: %s", key, err)
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
		return fmt.Errorf("fail to get active expectation item for %s when expecting: %s", key, err)
	}

	ae.recordTimestamp = time.Now()
	if !exist {
		item = &ActiveExpectationItem{Client: ae.Client, Name: name, Kind: kind, Key: key, Action: action, RecordTimestamp: time.Now()}
		if err := ae.items.Add(item); err != nil {
			return err
		}

		return nil
	}

	item.(*ActiveExpectationItem).Action = action
	item.(*ActiveExpectationItem).RecordTimestamp = time.Now()
	return nil
}

func (ae *ActiveExpectation) expectUpdate(kind ExpectedResourceType, name string, resourceVersion int64) error {
	key := fmt.Sprintf("%s/%s", kind, name)
	item, exist, err := ae.items.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to get active expectation item for %s when expecting: %s", key, err)
	}

	ae.recordTimestamp = time.Now()
	if !exist {
		item = &ActiveExpectationItem{Client: ae.Client, Name: name, Kind: kind, Key: key, Action: Update, ResourceVersion: resourceVersion, RecordTimestamp: time.Now()}
		if err := ae.items.Add(item); err != nil {
			return err
		}

		return nil
	}

	ea := item.(*ActiveExpectationItem)
	ea.Action = Update
	ea.ResourceVersion = resourceVersion
	ea.RecordTimestamp = time.Now()

	return nil
}

func (ae *ActiveExpectation) isSatisfied() (satisfied bool, err error) {
	items := ae.items.List()

	satisfied = true
	for _, item := range items {
		itemSatisfied, itemErr := func() (satisfied bool, err error) {
			defer func() {
				if satisfied {
					ae.items.Delete(item)
				} else if ae.recordTimestamp.Add(ExpectationsTimeout).Before(time.Now()) {
					panic("expected panic for active expectation")
				}
			}()

			satisfied, err = item.(*ActiveExpectationItem).isSatisfied(ae.namespace)
			if err != nil {
				return false, err
			}

			return satisfied, nil
		}()

		if itemErr != nil && err == nil {
			err = fmt.Errorf("fail to check satisfication for subject %s, item %s: %s", ae.key, item.(*ActiveExpectationItem).Key, err)
		}

		satisfied = satisfied && itemSatisfied
	}

	return satisfied, err
}

func (ae *ActiveExpectation) delete(kind, name string) error {
	key := fmt.Sprintf("%s/%s", kind, name)
	item, exist, err := ae.items.GetByKey(key)
	if err != nil {
		return fmt.Errorf("fail to delete active expectation item for %s: %s", key, err)
	}

	if !exist {
		return nil
	}

	if err := ae.items.Delete(item); err != nil {
		return fmt.Errorf("fail to do delete active expectation item for %s: %s", key, err)
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

func (i *ActiveExpectationItem) isSatisfied(namespace string) (bool, error) {
	switch i.Action {
	case Create:
		resource := ResourceInitializers[i.Kind]()
		if err := i.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: i.Name}, resource); err == nil {
			return true, nil
		} else if errors.IsNotFound(err) && i.RecordTimestamp.Add(30*time.Second).Before(time.Now()) {
			// tolerate watch event missing, after 30s
			return true, nil
		}
	case Delete:
		resource := ResourceInitializers[i.Kind]()
		if err := i.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: i.Name}, resource); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
		} else {
			if resource.(metav1.Object).GetDeletionTimestamp() != nil {
				return true, nil
			}
		}
	case Update:
		resource := ResourceInitializers[i.Kind]()
		if err := i.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: i.Name}, resource); err == nil {
			rv, err := strconv.ParseInt(resource.(metav1.Object).GetResourceVersion(), 10, 64)
			if err != nil {
				// true for error
				return true, nil
			}
			if rv >= i.ResourceVersion {
				return true, nil
			}
		} else {
			if errors.IsNotFound(err) {
				return true, nil
			}
		}
	}
	return false, nil
}

// ExpKeyFunc to parse out the key from a ControlleeExpectation
var ExpKeyFunc = func(obj interface{}) (string, error) {
	if e, ok := obj.(*ActiveExpectation); ok {
		return e.key, nil
	}
	panic(fmt.Sprintf("Could not find key for obj %#v", obj))
}
