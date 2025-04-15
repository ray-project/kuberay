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
// the original schema is defined here: https://github.com/apache/yunikorn-k8shim/blob/master/pkg/cache/amprotocol.go
type TaskGroup struct {
	MinResource               map[string]resource.Quantity      `json:"minResource"`
	NodeSelector              map[string]string                 `json:"nodeSelector,omitempty"`
	Affinity                  *corev1.Affinity                  `json:"affinity,omitempty"`
	Name                      string                            `json:"name"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	Tolerations               []corev1.Toleration               `json:"tolerations,omitempty"`
	MinMember                 int32                             `json:"minMember"`
}

func newTaskGroups() *TaskGroups {
	return &TaskGroups{
		Groups: make([]TaskGroup, 0),
	}
}

func newTaskGroupsFromCluster(cluster *v1.RayCluster) *TaskGroups {
	taskGroups := newTaskGroups()

	// head group
	headGroupSpec := cluster.Spec.HeadGroupSpec
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

	// worker groups
	for _, workerGroupSpec := range cluster.Spec.WorkerGroupSpecs {
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

func newTaskGroupsFromJob(app *v1.RayJob) *TaskGroups {
	taskGroups := newTaskGroups()

	// head group
	headGroupSpec := app.Spec.RayClusterSpec.HeadGroupSpec
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

	// worker groups
	for _, workerGroupSpec := range app.Spec.RayClusterSpec.WorkerGroupSpecs {
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

	jobSubmitterSpec := app.Spec.SubmitterPodTemplate
	if jobSubmitterSpec == nil {
		jobSubmitterSpec = &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("200Mi"),
							},
						},
					},
				},
			},
		}
	}
	jobSubmitterMinResource := utils.CalculatePodResource(jobSubmitterSpec.Spec)
	taskGroups.addTaskGroup(
		TaskGroup{
			Name:         utils.RaySubmitterGroupLabelValue,
			MinMember:    1,
			MinResource:  utils.ConvertResourceListToMapString(jobSubmitterMinResource),
			NodeSelector: jobSubmitterSpec.Spec.NodeSelector,
			Tolerations:  jobSubmitterSpec.Spec.Tolerations,
			Affinity:     jobSubmitterSpec.Spec.Affinity,
		})

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
