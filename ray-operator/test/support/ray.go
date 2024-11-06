package support

import (
	"errors"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
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

func GetRayService(t Test, namespace, name string) (*rayv1.RayService, error) {
	return t.Client().Ray().RayV1().RayServices(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
}

func RayClusterState(cluster *rayv1.RayCluster) rayv1.ClusterState {
	return cluster.Status.State //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
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
	assert.NoError(t.T(), err)
	return pods.Items
}

func RayService(t Test, namespace, name string) func(g gomega.Gomega) *rayv1.RayService {
	return func(g gomega.Gomega) *rayv1.RayService {
		service, err := t.Client().Ray().RayV1().RayServices(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return service
	}
}

func RayServiceStatus(service *rayv1.RayService) rayv1.ServiceStatus {
	return service.Status.ServiceStatus
}
