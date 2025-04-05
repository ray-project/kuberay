package generation

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
)

func TestGenerateRayClusterApplyConfig(t *testing.T) {
	labels := map[string]string{
		"blue":    "jay",
		"eastern": "bluebird",
	}
	annotations := map[string]string{
		"mourning":  "dove",
		"baltimore": "oriole",
	}

	testRayClusterSpecObject := RayClusterSpecObject{
		Name:        ptr.To("test-ray-cluster"),
		Namespace:   ptr.To("default"),
		Labels:      labels,
		Annotations: annotations,
		RayVersion:  ptr.To(util.RayVersion),
		Image:       ptr.To(util.RayImage),
		HeadCPU:     ptr.To("1"),
		HeadMemory:  ptr.To("5Gi"),
		HeadGPU:     ptr.To("1"),
		HeadRayStartParams: map[string]string{
			"dashboard-host": "1.2.3.4",
			"num-cpus":       "0",
		},
		WorkerGroups: []WorkerGroupConfig{
			{
				WorkerReplicas: ptr.To(int32(3)),
				WorkerCPU:      ptr.To("2"),
				WorkerMemory:   ptr.To("10Gi"),
				WorkerGPU:      ptr.To("1"),
				WorkerRayStartParams: map[string]string{
					"dagon":    "azathoth",
					"shoggoth": "cthulhu",
				},
			},
		},
	}
	expectedWorkerRayStartParams := map[string]string{
		"metrics-export-port": "8080",
	}
	maps.Copy(expectedWorkerRayStartParams, testRayClusterSpecObject.WorkerGroups[0].WorkerRayStartParams)

	result := testRayClusterSpecObject.GenerateRayClusterApplyConfig()

	assert.Equal(t, testRayClusterSpecObject.Name, result.Name)
	assert.Equal(t, testRayClusterSpecObject.Namespace, result.Namespace)
	assert.Equal(t, testRayClusterSpecObject.Labels, labels)
	assert.Equal(t, testRayClusterSpecObject.Annotations, annotations)
	assert.Equal(t, testRayClusterSpecObject.RayVersion, result.Spec.RayVersion)
	assert.Equal(t, testRayClusterSpecObject.Image, result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Image)
	assert.Equal(t, resource.MustParse(*testRayClusterSpecObject.HeadCPU), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(*testRayClusterSpecObject.HeadGPU), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Name(corev1.ResourceName("nvidia.com/gpu"), resource.DecimalSI))
	assert.Equal(t, resource.MustParse(*testRayClusterSpecObject.HeadMemory), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Memory())
	assert.Equal(t, testRayClusterSpecObject.HeadRayStartParams, result.Spec.HeadGroupSpec.RayStartParams)
	assert.Equal(t, "default-group", *result.Spec.WorkerGroupSpecs[0].GroupName)
	assert.Equal(t, *testRayClusterSpecObject.WorkerGroups[0].WorkerReplicas, *result.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, resource.MustParse(*testRayClusterSpecObject.WorkerGroups[0].WorkerCPU), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(*testRayClusterSpecObject.WorkerGroups[0].WorkerGPU), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Name(corev1.ResourceName("nvidia.com/gpu"), resource.DecimalSI))
	assert.Equal(t, resource.MustParse(*testRayClusterSpecObject.WorkerGroups[0].WorkerMemory), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Memory())
	assert.Equal(t, expectedWorkerRayStartParams, result.Spec.WorkerGroupSpecs[0].RayStartParams)
}

func TestGenerateRayJobApplyConfig(t *testing.T) {
	testRayJobYamlObject := RayJobYamlObject{
		RayJobName:     "test-ray-job",
		Namespace:      "default",
		SubmissionMode: "InteractiveMode",
		RayClusterSpecObject: RayClusterSpecObject{
			RayVersion: ptr.To(util.RayVersion),
			Image:      ptr.To(util.RayImage),
			HeadCPU:    ptr.To("1"),
			HeadGPU:    ptr.To("1"),
			HeadMemory: ptr.To("5Gi"),
			WorkerGroups: []WorkerGroupConfig{
				{
					WorkerReplicas: ptr.To(int32(3)),
					WorkerCPU:      ptr.To("2"),
					WorkerMemory:   ptr.To("10Gi"),
					WorkerGPU:      ptr.To("0"),
				},
			},
		},
	}

	result := testRayJobYamlObject.GenerateRayJobApplyConfig()

	assert.Equal(t, testRayJobYamlObject.RayJobName, *result.Name)
	assert.Equal(t, testRayJobYamlObject.Namespace, *result.Namespace)
	assert.Equal(t, rayv1.JobSubmissionMode(testRayJobYamlObject.SubmissionMode), *result.Spec.SubmissionMode)
	assert.Equal(t, testRayJobYamlObject.RayVersion, result.Spec.RayClusterSpec.RayVersion)
	assert.Equal(t, testRayJobYamlObject.Image, result.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Image)
	assert.Equal(t, resource.MustParse(*testRayJobYamlObject.HeadCPU), *result.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(*testRayJobYamlObject.HeadMemory), *result.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Memory())
	assert.Equal(t, resource.MustParse(*testRayJobYamlObject.HeadGPU), *result.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Name(corev1.ResourceName("nvidia.com/gpu"), resource.DecimalSI))
	assert.Equal(t, "default-group", *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].GroupName)
	assert.Equal(t, *testRayJobYamlObject.WorkerGroups[0].WorkerReplicas, *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, resource.MustParse(*testRayJobYamlObject.WorkerGroups[0].WorkerCPU), *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(*testRayJobYamlObject.WorkerGroups[0].WorkerMemory), *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Memory())
}

func TestConvertRayClusterApplyConfigToYaml(t *testing.T) {
	testRayClusterSpecObject := RayClusterSpecObject{
		Name:      ptr.To("test-ray-cluster"),
		Namespace: ptr.To("default"),
		Labels: map[string]string{
			"purple":     "finch",
			"red-tailed": "hawk",
		},
		Annotations: map[string]string{
			"american": "goldfinch",
			"piping":   "plover",
		},
		RayVersion: ptr.To(util.RayVersion),
		Image:      ptr.To(util.RayImage),
		HeadCPU:    ptr.To("1"),
		HeadMemory: ptr.To("5Gi"),
		HeadGPU:    ptr.To("1"),
		WorkerGroups: []WorkerGroupConfig{
			{
				WorkerReplicas: ptr.To(int32(3)),
				WorkerCPU:      ptr.To("2"),
				WorkerMemory:   ptr.To("10Gi"),
				WorkerGPU:      ptr.To("0"),
			},
		},
	}

	result := testRayClusterSpecObject.GenerateRayClusterApplyConfig()

	resultString, err := ConvertRayClusterApplyConfigToYaml(result)
	require.NoError(t, err)
	expectedResultYaml := fmt.Sprintf(`apiVersion: ray.io/v1
kind: RayCluster
metadata:
  annotations:
    american: goldfinch
    piping: plover
  labels:
    purple: finch
    red-tailed: hawk
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
              memory: 10Gi
            requests:
              cpu: "2"
              memory: 10Gi`, util.RayImage, util.RayVersion, util.RayImage)

	assert.Equal(t, strings.TrimSpace(expectedResultYaml), strings.TrimSpace(resultString))
}

func TestGenerateResources(t *testing.T) {
	tests := []struct {
		expectedRequestResources corev1.ResourceList
		expectedLimitResources   corev1.ResourceList
		cpu                      *string
		memory                   *string
		ephemeralStorage         *string
		gpu                      *string
		name                     string
	}{
		{
			name:             "should generate resources with CPU, memory, ephemeral storage, and GPU",
			cpu:              ptr.To("1"),
			memory:           ptr.To("5Gi"),
			ephemeralStorage: ptr.To("10Gi"),
			gpu:              ptr.To("1"),
			expectedRequestResources: corev1.ResourceList{
				corev1.ResourceCPU:                          resource.MustParse("1"),
				corev1.ResourceMemory:                       resource.MustParse("5Gi"),
				corev1.ResourceEphemeralStorage:             resource.MustParse("10Gi"),
				corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
			},
			expectedLimitResources: corev1.ResourceList{
				corev1.ResourceMemory:                       resource.MustParse("5Gi"),
				corev1.ResourceEphemeralStorage:             resource.MustParse("10Gi"),
				corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
			},
		},
		{
			name:   "should only generate resources with CPU and memory if ephemeral storage isn't set and GPUs are 0",
			cpu:    ptr.To("1"),
			memory: ptr.To("5Gi"),
			expectedRequestResources: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("5Gi"),
			},
			expectedLimitResources: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("5Gi"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expectedRequestResources, generateRequestResources(test.cpu, test.memory, test.ephemeralStorage, test.gpu))
			assert.Equal(t, test.expectedLimitResources, generateLimitResources(test.memory, test.ephemeralStorage, test.gpu))
		})
	}
}

func TestGenerateRayClusterSpec(t *testing.T) {
	testRayClusterSpecObject := RayClusterSpecObject{
		RayVersion:           ptr.To("1.2.3"),
		Image:                ptr.To("rayproject/ray:1.2.3"),
		HeadCPU:              ptr.To("1"),
		HeadMemory:           ptr.To("5Gi"),
		HeadGPU:              ptr.To("1"),
		HeadEphemeralStorage: ptr.To("10Gi"),
		HeadRayStartParams: map[string]string{
			"softmax": "GELU",
		},
		HeadNodeSelectors: map[string]string{
			"head-selector1": "foo",
			"head-selector2": "bar",
		},
		WorkerGroups: []WorkerGroupConfig{
			{
				WorkerReplicas: ptr.To(int32(3)),
				WorkerCPU:      ptr.To("2"),
				WorkerMemory:   ptr.To("10Gi"),
				WorkerGPU:      ptr.To("0"),
				WorkerRayStartParams: map[string]string{
					"metrics-export-port": "8080",
				},
				WorkerNodeSelectors: map[string]string{
					"worker-selector1": "baz",
					"worker-selector2": "qux",
				},
			},
		},
	}

	expected := &rayv1ac.RayClusterSpecApplyConfiguration{
		RayVersion: ptr.To("1.2.3"),
		HeadGroupSpec: &rayv1ac.HeadGroupSpecApplyConfiguration{
			RayStartParams: map[string]string{"dashboard-host": "0.0.0.0", "softmax": "GELU"},
			Template: &corev1ac.PodTemplateSpecApplyConfiguration{
				Spec: &corev1ac.PodSpecApplyConfiguration{
					Containers: []corev1ac.ContainerApplyConfiguration{
						{
							Name:  ptr.To("ray-head"),
							Image: ptr.To("rayproject/ray:1.2.3"),
							Resources: &corev1ac.ResourceRequirementsApplyConfiguration{
								Requests: &corev1.ResourceList{
									corev1.ResourceCPU:                          resource.MustParse("1"),
									corev1.ResourceMemory:                       resource.MustParse("5Gi"),
									corev1.ResourceEphemeralStorage:             resource.MustParse("10Gi"),
									corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
								},
								Limits: &corev1.ResourceList{
									corev1.ResourceMemory:                       resource.MustParse("5Gi"),
									corev1.ResourceEphemeralStorage:             resource.MustParse("10Gi"),
									corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
								},
							},
							Ports: []corev1ac.ContainerPortApplyConfiguration{
								{
									ContainerPort: ptr.To(int32(6379)),
									Name:          ptr.To("gcs-server"),
								},
								{
									ContainerPort: ptr.To(int32(8265)),
									Name:          ptr.To("dashboard"),
								},
								{
									ContainerPort: ptr.To(int32(10001)),
									Name:          ptr.To("client"),
								},
							},
						},
					},
					NodeSelector: map[string]string{
						"head-selector1": "foo",
						"head-selector2": "bar",
					},
				},
			},
		},
		WorkerGroupSpecs: []rayv1ac.WorkerGroupSpecApplyConfiguration{
			{
				GroupName:      ptr.To("default-group"),
				Replicas:       ptr.To(int32(3)),
				RayStartParams: map[string]string{"metrics-export-port": "8080"},
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					Spec: &corev1ac.PodSpecApplyConfiguration{
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name:  ptr.To("ray-worker"),
								Image: ptr.To("rayproject/ray:1.2.3"),
								Resources: &corev1ac.ResourceRequirementsApplyConfiguration{
									Requests: &corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("10Gi"),
									},
									Limits: &corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("10Gi"),
									},
								},
							},
						},
						NodeSelector: map[string]string{
							"worker-selector1": "baz",
							"worker-selector2": "qux",
						},
					},
				},
			},
		},
	}

	result := testRayClusterSpecObject.generateRayClusterSpec()

	assert.Equal(t, expected, result)
}
