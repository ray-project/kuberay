package koordinator

import (
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func generateGangGroupName(app *rayv1.RayCluster, namespace, groupName string) string {
	if namespace == "" {
		namespace = "default"
	}
	return namespace + "/" + app.Name + "-" + groupName
}

func newGangGroupsFromApp(app *rayv1.RayCluster) ([]string, map[string]int32) {
	gangGroups := make([]string, 1+len(app.Spec.WorkerGroupSpecs))
	minMemberMap := map[string]int32{}

	gangGroups[0] = generateGangGroupName(app, app.Spec.HeadGroupSpec.Template.Namespace, utils.RayNodeHeadGroupLabelValue)
	minMemberMap[utils.RayNodeHeadGroupLabelValue] = 1

	for i, workerGroupSepc := range app.Spec.WorkerGroupSpecs {
		minWorkers := workerGroupSepc.MinReplicas
		gangGroups[1+i] = generateGangGroupName(app, workerGroupSepc.Template.Namespace, workerGroupSepc.GroupName)
		minMemberMap[workerGroupSepc.GroupName] = *minWorkers

	}

	return gangGroups, minMemberMap
}
