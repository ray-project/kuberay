package koordinator

import (
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	workerOffset = 1
)

func generateGangGroupName(app *rayv1.RayCluster, namespace, groupName string) string {
	if namespace == "" {
		namespace = app.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}
	return namespace + "/" + getAppPodGroupName(app, groupName)
}

type wokerGroupReplicas struct {
	Replicas    int32
	MinReplicas int32
}

func analyzeGangGroupsFromApp(app *rayv1.RayCluster) ([]string, map[string]wokerGroupReplicas) {
	gangGroups := make([]string, len(app.Spec.WorkerGroupSpecs)+workerOffset)
	minMemberMap := map[string]wokerGroupReplicas{}

	gangGroups[0] = generateGangGroupName(app, app.Spec.HeadGroupSpec.Template.Namespace, utils.RayNodeHeadGroupLabelValue)
	minMemberMap[utils.RayNodeHeadGroupLabelValue] = wokerGroupReplicas{
		Replicas:    1,
		MinReplicas: 1,
	}

	for i, workerGroupSepc := range app.Spec.WorkerGroupSpecs {
		gangGroups[i+workerOffset] = generateGangGroupName(app, workerGroupSepc.Template.Namespace, workerGroupSepc.GroupName)
		minMemberMap[workerGroupSepc.GroupName] = wokerGroupReplicas{
			Replicas:    *workerGroupSepc.Replicas,
			MinReplicas: *workerGroupSepc.MinReplicas,
		}
	}

	return gangGroups, minMemberMap
}
