package support

import (
	"github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func RayJob(t Test, namespace, name string) func(g gomega.Gomega) *rayv1.RayJob {
	return func(g gomega.Gomega) *rayv1.RayJob {
		job, err := t.Client().Ray().RayV1().RayJobs(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return job
	}
}

func GetRayJob(t Test, namespace, name string) *rayv1.RayJob {
	t.T().Helper()
	return RayJob(t, namespace, name)(t)
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

func GetRayJobId(t Test, namespace, name string) string {
	t.T().Helper()
	job := RayJob(t, namespace, name)(t)
	return job.Status.JobId
}

func RayCluster(t Test, namespace, name string) func(g gomega.Gomega) *rayv1.RayCluster {
	return func(g gomega.Gomega) *rayv1.RayCluster {
		cluster, err := t.Client().Ray().RayV1().RayClusters(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return cluster
	}
}

func RayClusterOrError(t Test, namespace, name string) func(g gomega.Gomega) (*rayv1.RayCluster, error) {
	return func(_ gomega.Gomega) (*rayv1.RayCluster, error) {
		return t.Client().Ray().RayV1().RayClusters(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
	}
}

func GetRayCluster(t Test, namespace, name string) *rayv1.RayCluster {
	t.T().Helper()
	return RayCluster(t, namespace, name)(t)
}

func RayClusterState(cluster *rayv1.RayCluster) rayv1.ClusterState {
	return cluster.Status.State
}

func RayClusterDesiredWorkerReplicas(cluster *rayv1.RayCluster) int32 {
	return cluster.Status.DesiredWorkerReplicas
}

func GetHeadPod(t Test, rayCluster *rayv1.RayCluster) *corev1.Pod {
	t.T().Helper()
	pods, err := t.Client().Core().CoreV1().Pods(rayCluster.Namespace).List(
		t.Ctx(),
		common.RayClusterHeadPodsAssociationOptions(rayCluster).ToMetaV1ListOptions(),
	)
	t.Expect(err).NotTo(gomega.HaveOccurred())
	t.Expect(len(pods.Items)).To(gomega.Equal(1))
	return &pods.Items[0]
}

func GetGroupPods(t Test, rayCluster *rayv1.RayCluster, group string) []corev1.Pod {
	t.T().Helper()
	pods, err := t.Client().Core().CoreV1().Pods(rayCluster.Namespace).List(
		t.Ctx(),
		common.RayClusterGroupPodsAssociationOptions(rayCluster, group).ToMetaV1ListOptions(),
	)
	t.Expect(err).NotTo(gomega.HaveOccurred())
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
