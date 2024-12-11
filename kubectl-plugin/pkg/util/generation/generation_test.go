package generation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGenerateRayCluterApplyConfig(t *testing.T) {
	testRayClusterYamlObject := RayClusterYamlObject{
		ClusterName:    "test-ray-cluster",
		Namespace:      "default",
		RayVersion:     "2.39.0",
		Image:          "rayproject/ray:2.39.0",
		HeadCPU:        "1",
		HeadMemory:     "5Gi",
		WorkerGrpName:  "worker-group1",
		WorkerReplicas: 3,
		WorkerCPU:      "1",
		WorkerMemory:   "5Gi",
	}

	result, err := testRayClusterYamlObject.GenerateRayClusterApplyConfig()
	assert.Nil(t, err)

	assert.Equal(t, testRayClusterYamlObject.ClusterName, *result.Name)
	assert.Equal(t, testRayClusterYamlObject.Namespace, *result.Namespace)
	assert.Equal(t, testRayClusterYamlObject.RayVersion, *result.Spec.RayVersion)
	assert.Equal(t, testRayClusterYamlObject.Image, *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Image)
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.HeadCPU), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.HeadMemory), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Memory())
	assert.Equal(t, testRayClusterYamlObject.WorkerGrpName, *result.Spec.WorkerGroupSpecs[0].GroupName)
	assert.Equal(t, testRayClusterYamlObject.WorkerReplicas, *result.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.WorkerCPU), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(testRayClusterYamlObject.WorkerMemory), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Memory())
}

func TestConvertRayClusterApplyConfigToYaml(t *testing.T) {
	testRayClusterYamlObject := RayClusterYamlObject{
		ClusterName:    "test-ray-cluster",
		Namespace:      "default",
		RayVersion:     "2.39.0",
		Image:          "rayproject/ray:2.39.0",
		HeadCPU:        "1",
		HeadMemory:     "5Gi",
		WorkerGrpName:  "worker-group1",
		WorkerReplicas: 3,
		WorkerCPU:      "1",
		WorkerMemory:   "5Gi",
	}

	result, err := testRayClusterYamlObject.GenerateRayClusterApplyConfig()
	assert.Nil(t, err)

	resultString, err := ConvertRayClusterApplyConfigToYaml(result)
	assert.Nil(t, err)
	expectedResultYaml := `apiVersion: ray.io/v1
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
        - image: rayproject/ray:2.39.0
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
            requests:
              cpu: "1"
              memory: 5Gi
  rayVersion: 2.39.0
  workerGroupSpecs:
  - groupName: worker-group1
    rayStartParams:
      metrics-export-port: "8080"
    replicas: 3
    template:
      spec:
        containers:
        - image: rayproject/ray:2.39.0
          name: ray-worker
          resources:
            limits:
              cpu: "1"
              memory: 5Gi
            requests:
              cpu: "1"
              memory: 5Gi`

	assert.Equal(t, expectedResultYaml, strings.TrimSpace(resultString))
}
