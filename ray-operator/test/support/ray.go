package support

import (
	"errors"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
)

func RayJob(t Test, namespace, name string) func() (*rayv1.RayJob, error) {
	return func() (*rayv1.RayJob, error) {
		return GetRayJob(t, namespace, name)
	}
}

func GetRayJob(t Test, namespace, name string) (*rayv1.RayJob, error) {
	return t.Client().Ray().RayV1().RayJobs(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
}

func RayJobStatus(job *rayv1.RayJob) rayv1.JobStatus {
	return job.Status.JobStatus
}

func RayJobDeploymentStatus(job *rayv1.RayJob) rayv1.JobDeploymentStatus {
	return job.Status.JobDeploymentStatus
}

func RayJobManagedBy(job *rayv1.RayJob) *string {
	return job.Spec.ManagedBy
}

func RayJobReason(job *rayv1.RayJob) rayv1.JobFailedReason {
	return job.Status.Reason
}

func RayJobFailed(job *rayv1.RayJob) int32 {
	if job.Status.Failed == nil {
		return 0
	}
	return *job.Status.Failed
}

func RayJobSucceeded(job *rayv1.RayJob) int32 {
	if job.Status.Succeeded == nil {
		return 0
	}
	return *job.Status.Succeeded
}

func RayJobClusterName(job *rayv1.RayJob) string {
	return job.Status.RayClusterName
}

func RayCluster(t Test, namespace, name string) func() (*rayv1.RayCluster, error) {
	return func() (*rayv1.RayCluster, error) {
		return GetRayCluster(t, namespace, name)
	}
}

func GetRayCluster(t Test, namespace, name string) (*rayv1.RayCluster, error) {
	return t.Client().Ray().RayV1().RayClusters(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
}

func RayClusterState(cluster *rayv1.RayCluster) rayv1.ClusterState {
	return cluster.Status.State //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
}

func StatusCondition(condType rayv1.RayClusterConditionType) func(*rayv1.RayCluster) metav1.Condition {
	return func(cluster *rayv1.RayCluster) metav1.Condition {
		if cluster != nil {
			for _, cond := range cluster.Status.Conditions {
				if cond.Type == string(condType) {
					return cond
				}
			}
		}
		return metav1.Condition{}
	}
}

type ConditionMatcher struct {
	expected metav1.Condition
}

func (c *ConditionMatcher) Match(actual interface{}) (success bool, err error) {
	if actual == nil {
		return false, errors.New("<actual> should be a metav1.Condition but it is nil")
	}
	a, ok := actual.(metav1.Condition)
	if !ok {
		return false, errors.New("<actual> should be a metav1.Condition")
	}
	return a.Reason == c.expected.Reason && a.Status == c.expected.Status, nil
}

func (c *ConditionMatcher) FailureMessage(actual interface{}) (message string) {
	a := actual.(metav1.Condition)
	return format.Message(a, "to equal", c.expected)
}

func (c *ConditionMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	a := actual.(metav1.Condition)
	return format.Message(a, "not to equal", c.expected)
}

func MatchCondition(status metav1.ConditionStatus, reason string) types.GomegaMatcher {
	return &ConditionMatcher{expected: metav1.Condition{Status: status, Reason: reason}}
}

func RayClusterDesiredWorkerReplicas(cluster *rayv1.RayCluster) int32 {
	return cluster.Status.DesiredWorkerReplicas
}

func HeadPod(t Test, rayCluster *rayv1.RayCluster) func() (*corev1.Pod, error) {
	return func() (*corev1.Pod, error) {
		return GetHeadPod(t, rayCluster)
	}
}

func GetHeadPod(t Test, rayCluster *rayv1.RayCluster) (*corev1.Pod, error) {
	pods, err := t.Client().Core().CoreV1().Pods(rayCluster.Namespace).List(
		t.Ctx(),
		common.RayClusterHeadPodsAssociationOptions(rayCluster).ToMetaV1ListOptions(),
	)
	if err != nil {
		return nil, err
	}
	if len(pods.Items) != 1 {
		return nil, errors.New("number of head pods is not 1")
	}
	return &pods.Items[0], nil
}

func WorkerPods(t Test, rayCluster *rayv1.RayCluster) func() ([]corev1.Pod, error) {
	return func() ([]corev1.Pod, error) {
		return GetWorkerPods(t, rayCluster)
	}
}

func GetWorkerPods(t Test, rayCluster *rayv1.RayCluster) ([]corev1.Pod, error) {
	pods, err := t.Client().Core().CoreV1().Pods(rayCluster.Namespace).List(
		t.Ctx(),
		common.RayClusterWorkerPodsAssociationOptions(rayCluster).ToMetaV1ListOptions(),
	)
	if pods == nil {
		return nil, err
	}
	return pods.Items, err
}

func GetAllPods(t Test, rayCluster *rayv1.RayCluster) ([]corev1.Pod, error) {
	pods, err := t.Client().Core().CoreV1().Pods(rayCluster.Namespace).List(
		t.Ctx(),
		common.RayClusterAllPodsAssociationOptions(rayCluster).ToMetaV1ListOptions(),
	)
	if pods == nil {
		return nil, err
	}
	return pods.Items, err
}

func GetGroupPods(t Test, rayCluster *rayv1.RayCluster, group string) []corev1.Pod {
	t.T().Helper()
	pods, err := t.Client().Core().CoreV1().Pods(rayCluster.Namespace).List(
		t.Ctx(),
		common.RayClusterGroupPodsAssociationOptions(rayCluster, group).ToMetaV1ListOptions(),
	)
	require.NoError(t.T(), err)
	return pods.Items
}

func RayClusterManagedBy(rayCluster *rayv1.RayCluster) *string {
	return rayCluster.Spec.ManagedBy
}

func GetRayService(t Test, namespace, name string) (*rayv1.RayService, error) {
	return t.Client().Ray().RayV1().RayServices(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
}

func RayService(t Test, namespace, name string) func() (*rayv1.RayService, error) {
	return func() (*rayv1.RayService, error) {
		return GetRayService(t, namespace, name)
	}
}

func RayServiceStatus(service *rayv1.RayService) rayv1.ServiceStatus {
	return service.Status.ServiceStatus //nolint:staticcheck // `ServiceStatus` is deprecated
}

func IsRayServiceReady(service *rayv1.RayService) bool {
	return meta.IsStatusConditionTrue(service.Status.Conditions, string(rayv1.RayServiceReady))
}

func IsRayServiceUpgrading(service *rayv1.RayService) bool {
	return meta.IsStatusConditionTrue(service.Status.Conditions, string(rayv1.UpgradeInProgress))
}

func RayServicesNumEndPoints(service *rayv1.RayService) int32 {
	return service.Status.NumServeEndpoints
}

func GetRayClusterWorkerGroupReplicaSum(cluster *rayv1.RayCluster) int32 {
	var replicas int32
	for _, workerGroup := range cluster.Spec.WorkerGroupSpecs {
		replicas += *workerGroup.Replicas
	}
	return replicas
}
