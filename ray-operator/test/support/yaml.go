package support

import (
	"os"
	"os/exec"

	"github.com/stretchr/testify/assert"

	"k8s.io/apimachinery/pkg/runtime"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayscheme "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
)

func deserializeYAML(filename string, into runtime.Object) error {
	yamlFileContent, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	decoder := rayscheme.Codecs.UniversalDecoder()
	if _, _, err = decoder.Decode(yamlFileContent, nil, into); err != nil {
		return err
	}
	return nil
}

func DeserializeRayClusterYAML(t Test, filename string) *rayv1.RayCluster {
	t.T().Helper()
	rayCluster := &rayv1.RayCluster{}
	err := deserializeYAML(filename, rayCluster)
	assert.NoError(t.T(), err)
	return rayCluster
}

func DeserializeRayJobYAML(t Test, filename string) *rayv1.RayJob {
	t.T().Helper()
	rayJob := &rayv1.RayJob{}
	err := deserializeYAML(filename, rayJob)
	assert.NoError(t.T(), err)
	return rayJob
}

func DeserializeRayServiceYAML(t Test, filename string) *rayv1.RayService {
	t.T().Helper()
	rayService := &rayv1.RayService{}
	err := deserializeYAML(filename, rayService)
	assert.NoError(t.T(), err)
	return rayService
}

func KubectlApplyYAML(t Test, filename string, namespace string) {
	t.T().Helper()
	kubectlCmd := exec.CommandContext(t.Ctx(), "kubectl", "apply", "-f", filename, "-n", namespace)
	err := kubectlCmd.Run()
	if err != nil {
		t.T().Fatalf("Failed to apply %s to namespace %s: %v", filename, namespace, err)
	}
	t.T().Logf("Successfully applied %s to namespace %s", filename, namespace)
}

func KubectlApplyQuota(t Test, namespace, quota string) {
	t.T().Helper()
	kubectlCmd := exec.CommandContext(t.Ctx(), "kubectl", "create", "quota", namespace, "-n", namespace, quota)
	err := kubectlCmd.Run()
	assert.NoError(t.T(), err)
	t.T().Logf("Successfully applied quota %s in %s", quota, namespace)
}

func KubectlDeleteAllPods(t Test, namespace string) {
	t.T().Helper()
	kubectlCmd := exec.CommandContext(t.Ctx(), "kubectl", "delete", "--all", "pods", "-n", namespace)
	err := kubectlCmd.Run()
	assert.NoError(t.T(), err)
	t.T().Logf("Successfully delete pods in %s", namespace)
}
