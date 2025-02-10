package generation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestGenerateRayCluterApplyConfig(t *testing.T) {
	testRayClusterYamlObject := RayClusterYamlObject{
		ClusterName: "test-ray-cluster",
		Namespace:   "default",
		RayClusterSpecObject: RayClusterSpecObject{
			RayVersion:     util.RayVersion,
			Image:          util.RayImage,
			HeadCPU:        "1",
			HeadMemory:     "5Gi",
			HeadGPU:        "1",
			WorkerReplicas: 3,
			WorkerCPU:      "2",
			WorkerMemory:   "10Gi",
			WorkerGPU:      "1",
		},
	}

	result := testRayClusterYamlObject.GenerateRayClusterApplyConfig()

	assert.Equal(t, testRayClusterYamlObject.ClusterName, *result.Name)
	assert.Equal(t, testRayClusterYamlObject.Namespace, *result.Namespace)
	assert.Equal(t, testRayClusterYamlObject.RayVersion, *result.Spec.RayVersion)
	assert.Equal(t, testRayClusterYamlObject.Image, *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Image)
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.HeadCPU), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.HeadGPU), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Name(corev1.ResourceName("nvidia.com/gpu"), resource.DecimalSI))
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.HeadMemory), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Memory())
	assert.Equal(t, "default-group", *result.Spec.WorkerGroupSpecs[0].GroupName)
	assert.Equal(t, testRayClusterYamlObject.WorkerReplicas, *result.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.WorkerCPU), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.WorkerGPU), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Name(corev1.ResourceName("nvidia.com/gpu"), resource.DecimalSI))
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.WorkerMemory), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Memory())
}

func TestGenerateRayJobApplyConfig(t *testing.T) {
	testRayJobYamlObject := RayJobYamlObject{
		RayJobName:     "test-ray-job",
		Namespace:      "default",
		SubmissionMode: "InteractiveMode",
		RayClusterSpecObject: RayClusterSpecObject{
			RayVersion:     util.RayVersion,
			Image:          util.RayImage,
			HeadCPU:        "1",
			HeadGPU:        "1",
			HeadMemory:     "5Gi",
			WorkerReplicas: 3,
			WorkerCPU:      "2",
			WorkerMemory:   "10Gi",
			WorkerGPU:      "0",
		},
	}

	result := testRayJobYamlObject.GenerateRayJobApplyConfig()

	assert.Equal(t, testRayJobYamlObject.RayJobName, *result.Name)
	assert.Equal(t, testRayJobYamlObject.Namespace, *result.Namespace)
	assert.Equal(t, rayv1.JobSubmissionMode(testRayJobYamlObject.SubmissionMode), *result.Spec.SubmissionMode)
	assert.Equal(t, testRayJobYamlObject.RayVersion, *result.Spec.RayClusterSpec.RayVersion)
	assert.Equal(t, testRayJobYamlObject.Image, *result.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Image)
	assert.Equal(t, resource.MustParse(testRayJobYamlObject.HeadCPU), *result.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(testRayJobYamlObject.HeadMemory), *result.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Memory())
	assert.Equal(t, "default-group", *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].GroupName)
	assert.Equal(t, testRayJobYamlObject.WorkerReplicas, *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, resource.MustParse(testRayJobYamlObject.WorkerCPU), *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(testRayJobYamlObject.WorkerMemory), *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Memory())
}

func TestConvertRayClusterApplyConfigToYaml(t *testing.T) {
	testRayClusterYamlObject := RayClusterYamlObject{
		ClusterName: "test-ray-cluster",
		Namespace:   "default",
		RayClusterSpecObject: RayClusterSpecObject{
			RayVersion:     util.RayVersion,
			Image:          util.RayImage,
			HeadCPU:        "1",
			HeadMemory:     "5Gi",
			HeadGPU:        "1",
			WorkerReplicas: 3,
			WorkerCPU:      "2",
			WorkerMemory:   "10Gi",
			WorkerGPU:      "0",
		},
	}

	result := testRayClusterYamlObject.GenerateRayClusterApplyConfig()

	resultString, err := ConvertRayClusterApplyConfigToYaml(result)
	require.NoError(t, err)
	expectedResultYaml := fmt.Sprintf(`apiVersion: ray.io/v1
kind: RayCluster
metadata:
  name: test-ray-cluster
  namespace: default
spec:
  headGroupSpec:
    rayStartParams:
      dashboard-host: 0.0.0.0
    template:
      spec:
        containers:
        - image: %s
          name: ray-head
          ports:
          - containerPort: 6379
            name: gcs-server
          - containerPort: 8265
            name: dashboard
          - containerPort: 10001
            name: client
          resources:
            limits:
              cpu: "1"
              memory: 5Gi
              nvidia.com/gpu: "1"
            requests:
              cpu: "1"
              memory: 5Gi
              nvidia.com/gpu: "1"
  rayVersion: %s
  workerGroupSpecs:
  - groupName: default-group
    rayStartParams:
      metrics-export-port: "8080"
    replicas: 3
    template:
      spec:
        containers:
        - image: %s
          name: ray-worker
          resources:
            limits:
              cpu: "2"
              memory: 10Gi
            requests:
              cpu: "2"
              memory: 10Gi`, util.RayImage, util.RayVersion, util.RayImage)

	assert.Equal(t, strings.TrimSpace(expectedResultYaml), strings.TrimSpace(resultString))
}
