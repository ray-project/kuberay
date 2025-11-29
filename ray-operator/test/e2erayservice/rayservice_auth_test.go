package e2erayservice

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceAuthToken(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Create the RayService for testing with auth token using programmatic configuration
	rayServiceName := "rayservice-auth"
	rayServiceSpec := RayServiceSampleYamlApplyConfiguration()
	rayServiceSpec.RayClusterSpec.WithAuthOptions(rayv1ac.AuthOptions().WithMode(rayv1.AuthModeToken))

	// Add a worker group to verify auth token propagation to workers
	workerGroupSpec := rayv1ac.WorkerGroupSpec().
		WithGroupName("small-group").
		WithReplicas(1).
		WithMinReplicas(1).
		WithMaxReplicas(1).
		WithRayStartParams(map[string]string{"num-cpus": "1"}).
		WithTemplate(corev1ac.PodTemplateSpec().
			WithSpec(corev1ac.PodSpec().
				WithContainers(corev1ac.Container().
					WithName("ray-worker").
					WithImage(GetRayImage()).
					WithResources(corev1ac.ResourceRequirements().
						WithRequests(corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						}).
						WithLimits(corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						})))))
	rayServiceSpec.RayClusterSpec.WithWorkerGroupSpecs(workerGroupSpec)

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(rayServiceSpec)

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())
	LogWithTimestamp(test.T(), "Created RayService %s/%s successfully with AuthModeToken", rayService.Namespace, rayService.Name)

	// Wait for RayService to be ready
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Get the RayService
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "RayService %s/%s is ready", rayService.Namespace, rayService.Name)

	// Get the underlying RayCluster of the RayService
	rayClusterName := rayService.Status.ActiveServiceStatus.RayClusterName
	g.Expect(rayClusterName).NotTo(BeEmpty(), "RayCluster name should be populated")
	LogWithTimestamp(test.T(), "RayService %s/%s has active RayCluster %s", rayService.Namespace, rayService.Name, rayClusterName)

	// Wait for the RayCluster to become ready
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", namespace.Name, rayClusterName)
	g.Eventually(RayCluster(test, namespace.Name, rayClusterName), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	rayCluster, err := GetRayCluster(test, namespace.Name, rayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the head pod has auth token environment variables
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod).NotTo(BeNil())
	LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// Verify Ray container has auth token env vars
	VerifyContainerAuthTokenEnvVars(test, rayCluster, &headPod.Spec.Containers[utils.RayContainerIndex])
	LogWithTimestamp(test.T(), "Verified auth token env vars in head pod Ray container")

	// Verify worker pods have auth token env vars
	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workerPods).ToNot(BeEmpty(), "RayCluster should have at least one worker pod")
	LogWithTimestamp(test.T(), "Found %d worker pod(s)", len(workerPods))

	for _, workerPod := range workerPods {
		VerifyContainerAuthTokenEnvVars(test, rayCluster, &workerPod.Spec.Containers[utils.RayContainerIndex])
		LogWithTimestamp(test.T(), "Verified auth token env vars in worker pod %s/%s", workerPod.Namespace, workerPod.Name)
	}

	// Clean up the RayService
	err = test.Client().Ray().RayV1().RayServices(namespace.Name).Delete(test.Ctx(), rayService.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Deleted RayService %s/%s successfully", rayService.Namespace, rayService.Name)
}