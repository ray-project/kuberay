package yunikorn

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// TaskGroups is a list of task Groups recognized as gang Groups
type TaskGroups struct {
	Groups []TaskGroup `json:"groups"`
}

// TaskGroup is the struct for yunikorn to consider a pod belongs to a gang group
type TaskGroup struct {
	Name                      string                            `json:"name"`
	MinResource               map[string]resource.Quantity      `json:"minResource"`
	NodeSelector              map[string]string                 `json:"nodeSelector,omitempty"`
	Tolerations               []corev1.Toleration               `json:"tolerations,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	Affinity                  *corev1.Affinity                  `json:"affinity,omitempty"`
	MinMember                 int32                             `json:"minMember"`
}

func newTaskGroups() *TaskGroups {
	return &TaskGroups{
		Groups: make([]TaskGroup, 0),
	}
}

func newTaskGroupsFromApp(app *v1.RayCluster) *TaskGroups {
	taskGroups := newTaskGroups()

	// head group
	headGroupSpec := app.Spec.HeadGroupSpec
	headPodMinResource := utils.CalculatePodResource(headGroupSpec.Template.Spec)
	taskGroups.addTaskGroup(
		TaskGroup{
			Name:         utils.RayNodeHeadGroupLabelValue,
			MinMember:    1,
			MinResource:  utils.ConvertResourceListToMapString(headPodMinResource),
			NodeSelector: headGroupSpec.Template.Spec.NodeSelector,
			Tolerations:  headGroupSpec.Template.Spec.Tolerations,
			Affinity:     headGroupSpec.Template.Spec.Affinity,
		})

	// worker Groups
	for _, workerGroupSpec := range app.Spec.WorkerGroupSpecs {
		workerMinResource := utils.CalculatePodResource(workerGroupSpec.Template.Spec)
		minWorkers := workerGroupSpec.MinReplicas
		taskGroups.addTaskGroup(
			TaskGroup{
				Name:         workerGroupSpec.GroupName,
				MinMember:    *minWorkers,
				MinResource:  utils.ConvertResourceListToMapString(workerMinResource),
				NodeSelector: workerGroupSpec.Template.Spec.NodeSelector,
				Tolerations:  workerGroupSpec.Template.Spec.Tolerations,
				Affinity:     workerGroupSpec.Template.Spec.Affinity,
			})
	}

	return taskGroups
}

func (t *TaskGroups) size() int {
	return len(t.Groups)
}

func (t *TaskGroups) addTaskGroup(taskGroup TaskGroup) {
	t.Groups = append(t.Groups, taskGroup)
}

func (t *TaskGroups) marshal() (string, error) {
	result, err := json.Marshal(t.Groups)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func (t *TaskGroups) unmarshalFrom(spec string) error {
	return json.Unmarshal([]byte(spec), &t.Groups)
}

func (t *TaskGroups) getTaskGroup(name string) TaskGroup {
	for _, group := range t.Groups {
		if group.Name == name {
			return group
		}
	}
	return TaskGroup{}
}
