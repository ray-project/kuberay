package support

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stretchr/testify/require"
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
	require.NoError(t.T(), err, "Fail to deserialize yaml file %s", filename)
	return rayCluster
}

func DeserializeRayJobYAML(t Test, filename string) *rayv1.RayJob {
	t.T().Helper()
	rayJob := &rayv1.RayJob{}
	err := deserializeYAML(filename, rayJob)
	require.NoError(t.T(), err, "Fail to deserialize yaml file %s", filename)
	return rayJob
}

func DeserializeRayServiceYAML(t Test, filename string) *rayv1.RayService {
	t.T().Helper()
	rayService := &rayv1.RayService{}
	err := deserializeYAML(filename, rayService)
	require.NoError(t.T(), err, "Fail to deserialize yaml file %s", filename)
	return rayService
}

func KubectlApplyYAML(t Test, filename string, namespace string) {
	t.T().Helper()

	// Check if we should transform the YAML for a custom Ray image
	rayImage := GetRayImage()
	defaultImage := RayImage // from defaults.go

	if rayImage != defaultImage {
		LogWithTimestamp(t.T(), "Using kustomize to transform %s with Ray image: %s", filename, rayImage)
		kubectlApplyYAMLWithKustomize(t, filename, namespace, rayImage)
		return
	}

	kubectlCmd := exec.CommandContext(t.Ctx(), "kubectl", "apply", "-f", filename, "-n", namespace)
	err := kubectlCmd.Run()
	require.NoError(t.T(), err, "Failed to apply %s to namespace %s", filename, namespace)
	LogWithTimestamp(t.T(), "Successfully applied %s to namespace %s", filename, namespace)
}

// kubectlApplyYAMLWithKustomize creates a temporary kustomization that transforms the Ray image
// in the given YAML file, then applies it to the cluster.
func kubectlApplyYAMLWithKustomize(t Test, filename string, namespace string, rayImage string) {
	t.T().Helper()

	// Get absolute path to the source file
	absFilename, err := filepath.Abs(filename)
	require.NoError(t.T(), err, "Failed to get absolute path for %s", filename)

	// Create a temporary directory for kustomize
	tempDir, err := os.MkdirTemp("", "kuberay-kustomize-*")
	require.NoError(t.T(), err, "Failed to create temp directory for kustomize")
	defer os.RemoveAll(tempDir)

	// Copy the source YAML file to the temp directory
	sourceContent, err := os.ReadFile(absFilename)
	require.NoError(t.T(), err, "Failed to read %s", filename)

	baseFilename := filepath.Base(filename)
	destFile := filepath.Join(tempDir, baseFilename)
	err = os.WriteFile(destFile, sourceContent, 0o600)
	require.NoError(t.T(), err, "Failed to write %s to temp directory", baseFilename)

	// Extract image name and tag from rayImage (e.g., "rayproject/ray:nightly" -> name="rayproject/ray", tag="nightly")
	// If no tag is specified use the whole string as the image name
	var imageName, imageTag string
	if idx := strings.LastIndex(rayImage, ":"); idx != -1 {
		imageName = rayImage[:idx]
		imageTag = rayImage[idx+1:]
	} else {
		imageName = rayImage
		imageTag = ""
	}

	// Generate kustomization.yaml
	// Only include newTag if a tag was specified
	var imagesSection string
	if imageTag != "" {
		imagesSection = fmt.Sprintf(`images:
  - name: rayproject/ray
    newName: %s
    newTag: %s`, imageName, imageTag)
	} else {
		imagesSection = fmt.Sprintf(`images:
  - name: rayproject/ray
    newName: %s`, imageName)
	}

	kustomizationContent := fmt.Sprintf(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - %s

%s
`, baseFilename, imagesSection)

	kustomizationFile := filepath.Join(tempDir, "kustomization.yaml")
	err = os.WriteFile(kustomizationFile, []byte(kustomizationContent), 0o600)
	require.NoError(t.T(), err, "Failed to write kustomization.yaml")

	// Run kustomize build
	kustomizeCmd := exec.CommandContext(t.Ctx(), "kustomize", "build", tempDir)
	var stdout, stderr bytes.Buffer
	kustomizeCmd.Stdout = &stdout
	kustomizeCmd.Stderr = &stderr
	err = kustomizeCmd.Run()
	require.NoError(t.T(), err, "Kustomize build failed: %s", stderr.String())

	// Apply the kustomize output
	kubectlCmd := exec.CommandContext(t.Ctx(), "kubectl", "apply", "-n", namespace, "-f", "-")
	kubectlCmd.Stdin = strings.NewReader(stdout.String())
	output, err := kubectlCmd.CombinedOutput()
	require.NoError(t.T(), err, "Failed to apply kustomize output for %s: %s", filename, string(output))
	LogWithTimestamp(t.T(), "Successfully applied %s with kustomized Ray image %s to namespace %s", filename, rayImage, namespace)
}

func KubectlApplyQuota(t Test, namespace, quota string) {
	t.T().Helper()
	kubectlCmd := exec.CommandContext(t.Ctx(), "kubectl", "create", "quota", namespace, "-n", namespace, quota)
	err := kubectlCmd.Run()
	require.NoError(t.T(), err, "Failed to apply quota %s in %s", quota, namespace)
	LogWithTimestamp(t.T(), "Successfully applied quota %s in %s", quota, namespace)
}

func KubectlDeleteAllPods(t Test, namespace string) {
	t.T().Helper()
	kubectlCmd := exec.CommandContext(t.Ctx(), "kubectl", "delete", "--all", "pods", "-n", namespace)
	err := kubectlCmd.Run()
	require.NoError(t.T(), err, "Failed to delete pods in %s", namespace)
	LogWithTimestamp(t.T(), "Successfully delete pods in %s", namespace)
}
