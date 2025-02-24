package e2e

import (
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestStaticRayService(t *testing.T) {
	rayserviceYamlFile := "testdata/rayservice.static.yaml"
	locustYamlFile := "testdata/locust-cluster.const-rate.yaml"

	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Create a ConfigMap with Locust runner script
	configMapAC := newConfigMap(namespace.Name, files(test, "locust_runner.py"))
	configMap, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), configMapAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", configMap.Namespace, configMap.Name)

	// Create the RayService for testing
	KubectlApplyYAML(test, rayserviceYamlFile, namespace.Name)
	rayService, err := GetRayService(test, namespace.Name, "test-rayservice")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Create Locust RayCluster
	KubectlApplyYAML(test, locustYamlFile, namespace.Name)
	locustCluster, err := GetRayCluster(test, namespace.Name, "locust-cluster")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created Locust RayCluster %s/%s successfully", locustCluster.Namespace, locustCluster.Name)

	g.Eventually(RayCluster(test, locustCluster.Namespace, locustCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Expect(GetRayCluster(test, locustCluster.Namespace, locustCluster.Name)).To(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))

	headPod, err := GetHeadPod(test, locustCluster)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// Install Locust in the head Pod
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"pip", "install", "locust==2.32.10"})

	// Run Locust test
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{
		"python", "/locust-runner/locust_runner.py", "-f", "/locustfile/locustfile.py", "--host", "http://test-rayservice-serve-svc:8000",
	})
}

func TestAutoscalingRayService(t *testing.T) {
	rayserviceYamlFile := "testdata/rayservice.autoscaling.yaml"
	locustYamlFile := "testdata/locust-cluster.burst.yaml"
	numberOfPodsWhenSteady := 1

	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Create a ConfigMap with Locust runner script
	configMapAC := newConfigMap(namespace.Name, files(test, "locust_runner.py"))
	configMap, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), configMapAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", configMap.Namespace, configMap.Name)

	// Create the RayService for testing
	KubectlApplyYAML(test, rayserviceYamlFile, namespace.Name)
	rayService, err := GetRayService(test, namespace.Name, "test-rayservice")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Get the underlying RayCluster of the RayService
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())
	rayServiceUnderlyingRayCluster, err := GetRayCluster(test, namespace.Name, rayService.Status.ActiveServiceStatus.RayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	// Check the number of worker pods is correct when RayService is steady
	g.Eventually(WorkerPods(test, rayServiceUnderlyingRayCluster), TestTimeoutShort).Should(HaveLen(numberOfPodsWhenSteady))

	// Create Locust RayCluster
	KubectlApplyYAML(test, locustYamlFile, namespace.Name)
	locustCluster, err := GetRayCluster(test, namespace.Name, "locust-cluster")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created Locust RayCluster %s/%s successfully", locustCluster.Namespace, locustCluster.Name)

	g.Eventually(RayCluster(test, locustCluster.Namespace, locustCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Expect(GetRayCluster(test, locustCluster.Namespace, locustCluster.Name)).To(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))

	headPod, err := GetHeadPod(test, locustCluster)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// Install Locust in the head Pod
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"pip", "install", "locust==2.32.10"})

	// Run Locust test
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{
		"python", "/locust-runner/locust_runner.py", "-f", "/locustfile/locustfile.py", "--host", "http://test-rayservice-serve-svc:8000",
	})

	// Check the number of worker pods is more when RayService right after the burst
	pods, err := GetWorkerPods(test, rayServiceUnderlyingRayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(pods)).Should(BeNumerically(">", numberOfPodsWhenSteady))

	// Check the number of worker pods is correct when RayService is steady
	g.Eventually(WorkerPods(test, rayServiceUnderlyingRayCluster), TestTimeoutLong).Should(HaveLen(numberOfPodsWhenSteady))
}

func TestRayServiceZeroDowntimeUpgrade(t *testing.T) {
	rayserviceYamlFile := "testdata/rayservice.static.yaml"
	locustYamlFile := "testdata/locust-cluster.const-rate.yaml"

	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Create a ConfigMap with Locust runner script
	configMapAC := newConfigMap(namespace.Name, files(test, "locust_runner.py"))
	configMap, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), configMapAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", configMap.Namespace, configMap.Name)

	// Create the RayService for testing
	KubectlApplyYAML(test, rayserviceYamlFile, namespace.Name)
	rayService, err := GetRayService(test, namespace.Name, "test-rayservice")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Create Locust RayCluster
	KubectlApplyYAML(test, locustYamlFile, namespace.Name)
	locustCluster, err := GetRayCluster(test, namespace.Name, "locust-cluster")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created Locust RayCluster %s/%s successfully", locustCluster.Namespace, locustCluster.Name)

	g.Eventually(RayCluster(test, locustCluster.Namespace, locustCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Expect(GetRayCluster(test, locustCluster.Namespace, locustCluster.Name)).To(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))

	headPod, err := GetHeadPod(test, locustCluster)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// Install Locust in the head Pod
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"pip", "install", "locust==2.32.10"})

	// Start a goroutine to perform zero-downtime upgrade
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		test.T().Logf("Waiting several seconds before updating RayService")
		time.Sleep(30 * time.Second)

		test.T().Logf("Updating RayService")
		rayService, err := GetRayService(test, namespace.Name, "test-rayservice")
		g.Expect(err).NotTo(HaveOccurred())
		rayClusterName := rayService.Status.ActiveServiceStatus.RayClusterName

		newRayService := rayService.DeepCopy()
		newRayService.Spec.RayClusterSpec.RayVersion = ""
		newRayService, err = test.Client().Ray().RayV1().RayServices(newRayService.Namespace).Update(test.Ctx(), newRayService, metav1.UpdateOptions{})
		g.Expect(err).NotTo(HaveOccurred())

		test.T().Logf("Waiting for RayService %s/%s UpgradeInProgress condition to be true", newRayService.Namespace, newRayService.Name)
		g.Eventually(RayService(test, newRayService.Namespace, newRayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

		// Assert that the active RayCluster is eventually different
		test.T().Logf("Waiting for RayService %s/%s to switch to a new cluster", newRayService.Namespace, newRayService.Name)
		g.Eventually(RayService(test, newRayService.Namespace, newRayService.Name), TestTimeoutShort).Should(WithTransform(func(rayService *rayv1.RayService) string {
			return rayService.Status.ActiveServiceStatus.RayClusterName
		}, Not(Equal(rayClusterName))))

		test.T().Logf("Verifying RayService %s/%s UpgradeInProgress condition to be false", newRayService.Namespace, newRayService.Name)
		rayService, err = GetRayService(test, namespace.Name, "test-rayservice")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(IsRayServiceUpgrading(rayService)).To(BeFalse())
	}()

	// Run Locust test
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{
		"python", "/locust-runner/locust_runner.py", "-f", "/locustfile/locustfile.py", "--host", "http://test-rayservice-serve-svc:8000",
	})

	wg.Wait()
}

func TestRayServiceGCSFaultTolerance(t *testing.T) {
	rayserviceYamlFile := "testdata/ray-service.ft.yaml"
	locustYamlFile := "testdata/locust-cluster.const-rate.yaml"

	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Create a ConfigMap with Locust runner script
	configMapAC := newConfigMap(namespace.Name, files(test, "locust_runner.py"))
	configMap, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), configMapAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", configMap.Namespace, configMap.Name)

	// Create the RayService for testing
	KubectlApplyYAML(test, rayserviceYamlFile, namespace.Name)
	rayService, err := GetRayService(test, namespace.Name, "test-rayservice")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).
		Should(WithTransform(RayServicesNumEndPoints, Equal(int32(1))))

	// Get the underlying RayCluster of the RayService
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())
	rayServiceUnderlyingRayCluster, err := GetRayCluster(test, namespace.Name, rayService.Status.ActiveServiceStatus.RayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	// Create Locust RayCluster
	KubectlApplyYAML(test, locustYamlFile, namespace.Name)
	locustCluster, err := GetRayCluster(test, namespace.Name, "locust-cluster")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created Locust RayCluster %s/%s successfully", locustCluster.Namespace, locustCluster.Name)

	g.Eventually(RayCluster(test, locustCluster.Namespace, locustCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Expect(GetRayCluster(test, locustCluster.Namespace, locustCluster.Name)).To(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))

	locustHeadPod, err := GetHeadPod(test, locustCluster)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Found head pod %s/%s", locustHeadPod.Namespace, locustHeadPod.Name)

	// Install Locust in the Locust head Pod
	ExecPodCmd(test, locustHeadPod, common.RayHeadContainer, []string{"pip", "install", "locust==2.32.10"})

	// Get current head pod
	oldHeadPod, err := GetHeadPod(test, rayServiceUnderlyingRayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	// Store the name of the head Pod in a variable
	oldHeadPodName := oldHeadPod.Name
	// Kill gcs server
	ExecPodCmd(test, oldHeadPod, common.RayHeadContainer, []string{"pkill", "gcs_server"})
	// wait for head pod not to be ready
	g.Eventually(HeadPod(test, rayServiceUnderlyingRayCluster), TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeFalse()))

	startTime := time.Now()
	// Run Locust test
	ExecPodCmd(test, locustHeadPod, common.RayHeadContainer, []string{
		"python", "/locust-runner/locust_runner.py", "-f", "/locustfile/locustfile.py", "--host", "http://test-rayservice-serve-svc:8000",
	})
	// Because this test shares the Locust RayCluster YAML file with other tests,
	// we need to ensure the YAML file is not accidentally updated.
	g.Expect(time.Since(startTime) > 2*time.Minute).To(BeTrue())

	newHeadPod, err := GetHeadPod(test, rayServiceUnderlyingRayCluster)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(newHeadPod.Name).To(Equal(oldHeadPodName))
	g.Expect(newHeadPod.Status.ContainerStatuses[0].RestartCount).To(Equal(int32(1)))
	// Verify that all pods are running
	g.Expect(GetHeadPod(test, rayServiceUnderlyingRayCluster)).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))
	g.Expect(GetWorkerPods(test, rayServiceUnderlyingRayCluster)).Should(WithTransform(sampleyaml.AllPodsRunningAndReady, BeTrue()))
}
