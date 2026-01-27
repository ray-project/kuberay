package support

import (
	"fmt"
	"strings"

	. "github.com/onsi/gomega"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	RayClusterManifestPath = "../../config/raycluster.yaml"
	RayClusterID           = "default"
)

// ApplyRayClusterWithCollectorWithEnvs deploys a Ray cluster with the collector sidecar into the test namespace,
// adding the specified environment variables to the head pod.
func ApplyRayClusterWithCollectorWithEnvs(test Test, g *WithT, namespace *corev1.Namespace, envs map[string]string) *rayv1.RayCluster {
	rayClusterFromYaml := DeserializeRayClusterYAML(test, RayClusterManifestPath)
	rayClusterFromYaml.Namespace = namespace.Name

	headContainer := &rayClusterFromYaml.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex]
	if len(headContainer.Env) == 0 {
		headContainer.Env = []corev1.EnvVar{}
	}

	for key, value := range envs {
		env := corev1.EnvVar{
			Name:  key,
			Value: value,
		}
		headContainer.Env = append(headContainer.Env, env)
	}

	rayCluster, err := test.Client().Ray().RayV1().
		RayClusters(namespace.Name).
		Create(test.Ctx(), rayClusterFromYaml, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutLong).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	LogWithTimestamp(test.T(), "Waiting for head pod of RayCluster %s/%s to be running and ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
		Should(WithTransform(IsPodRunningAndReady, BeTrue()))

	return rayCluster
}

// injectCollectorRayClusterID injects the ray-cluster-id argument into all collector containers.
func injectCollectorRayClusterID(containers []corev1.Container, rayClusterID string) {
	for i := range containers {
		if containers[i].Name == "collector" {
			containers[i].Command = append(
				containers[i].Command,
				fmt.Sprintf("--ray-cluster-id=%s", rayClusterID),
			)
		}
	}
}

// GetSessionIDFromHeadPod retrieves the sessionID from the Ray head pod by reading the symlink
// /tmp/ray/session_latest and getting its basename.
func GetSessionIDFromHeadPod(test Test, g *WithT, rayCluster *rayv1.RayCluster) string {
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())

	getSessionIDCmd := `if [ -L "/tmp/ray/session_latest" ]; then
  session_path=$(readlink /tmp/ray/session_latest)
  basename "$session_path"
else
  echo "session_latest is not a symlink"
  exit 1
fi`
	output, _ := ExecPodCmd(test, headPod, "ray-head", []string{"sh", "-c", getSessionIDCmd})

	// Parse output to extract the sessionID.
	sessionID := strings.TrimSpace(output.String())
	LogWithTimestamp(test.T(), "Retrieved sessionID: %s", sessionID)
	g.Expect(sessionID).NotTo(BeEmpty(), "sessionID should not be empty")

	return sessionID
}

// GetNodeIDFromHeadPod retrieves the nodeID from the Ray head pod by reading /tmp/ray/raylet_node_id.
func GetNodeIDFromHeadPod(test Test, g *WithT, rayCluster *rayv1.RayCluster) string {
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())

	getNodeIDCmd := `if [ -f "/tmp/ray/raylet_node_id" ]; then
  cat /tmp/ray/raylet_node_id
else
  echo "raylet_node_id not found"
  exit 1
fi`
	output, _ := ExecPodCmd(test, headPod, "ray-head", []string{"sh", "-c", getNodeIDCmd})

	nodeID := strings.TrimSpace(output.String())
	LogWithTimestamp(test.T(), "Retrieved nodeID: %s", nodeID)
	g.Expect(nodeID).NotTo(BeEmpty(), "nodeID should not be empty")

	return nodeID
}

// GetNodeIDFromPod retrieves the nodeID from the Ray head or worker pod by reading /tmp/ray/raylet_node_id.
func GetNodeIDFromPod(test Test, g *WithT, getPod func() (*corev1.Pod, error), containerName string) string {
	pod, err := getPod()
	g.Expect(err).NotTo(HaveOccurred())

	getNodeIDCmd := `if [ -f "/tmp/ray/raylet_node_id" ]; then
  cat /tmp/ray/raylet_node_id
else
  echo "raylet_node_id not found"
  exit 1
fi`
	output, _ := ExecPodCmd(test, pod, containerName, []string{"sh", "-c", getNodeIDCmd})

	// Parse output to extract the nodeID.
	nodeID := strings.TrimSpace(output.String())
	LogWithTimestamp(test.T(), "Retrieved nodeID: %s", nodeID)
	g.Expect(nodeID).NotTo(BeEmpty(), "nodeID should not be empty")

	return nodeID
}

// FirstWorkerPod returns a function that gets the first worker pod from the Ray cluster.
// It adapts the WorkerPods function to be used with functions expecting a single pod.
func FirstWorkerPod(test Test, rayCluster *rayv1.RayCluster) func() (*corev1.Pod, error) {
	return func() (*corev1.Pod, error) {
		workerPods, err := GetWorkerPods(test, rayCluster)
		if err != nil {
			return nil, err
		}
		if len(workerPods) == 0 {
			return nil, fmt.Errorf("no worker pods found")
		}
		return &workerPods[0], nil
	}
}
