package generation

import (
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1 "k8s.io/client-go/applyconfigurations/meta/v1"
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

	testRayClusterConfig := RayClusterConfig{
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
		WorkerGroups: []WorkerGroup{
			{
				WorkerReplicas: int32(3),
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
	maps.Copy(expectedWorkerRayStartParams, testRayClusterConfig.WorkerGroups[0].WorkerRayStartParams)

	result := testRayClusterConfig.GenerateRayClusterApplyConfig()

	assert.Equal(t, testRayClusterConfig.Name, result.Name)
	assert.Equal(t, testRayClusterConfig.Namespace, result.Namespace)
	assert.Equal(t, testRayClusterConfig.Labels, labels)
	assert.Equal(t, testRayClusterConfig.Annotations, annotations)
	assert.Equal(t, testRayClusterConfig.RayVersion, result.Spec.RayVersion)
	assert.Equal(t, testRayClusterConfig.Image, result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Image)
	assert.Equal(t, resource.MustParse(*testRayClusterConfig.HeadCPU), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(*testRayClusterConfig.HeadGPU), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Name(corev1.ResourceName("nvidia.com/gpu"), resource.DecimalSI))
	assert.Equal(t, resource.MustParse(*testRayClusterConfig.HeadMemory), *result.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Memory())
	assert.Equal(t, testRayClusterConfig.HeadRayStartParams, result.Spec.HeadGroupSpec.RayStartParams)
	assert.Equal(t, "default-group", *result.Spec.WorkerGroupSpecs[0].GroupName)
	assert.Equal(t, testRayClusterConfig.WorkerGroups[0].WorkerReplicas, *result.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, resource.MustParse(*testRayClusterConfig.WorkerGroups[0].WorkerCPU), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(*testRayClusterConfig.WorkerGroups[0].WorkerGPU), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Name(corev1.ResourceName("nvidia.com/gpu"), resource.DecimalSI))
	assert.Equal(t, resource.MustParse(*testRayClusterConfig.WorkerGroups[0].WorkerMemory), *result.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Memory())
	assert.Equal(t, expectedWorkerRayStartParams, result.Spec.WorkerGroupSpecs[0].RayStartParams)
}

func TestGenerateRayJobApplyConfig(t *testing.T) {
	testRayJobYamlObject := RayJobYamlObject{
		RayJobName:     "test-ray-job",
		Namespace:      "default",
		SubmissionMode: "InteractiveMode",
		RayClusterConfig: RayClusterConfig{
			RayVersion: ptr.To(util.RayVersion),
			Image:      ptr.To(util.RayImage),
			HeadCPU:    ptr.To("1"),
			HeadGPU:    ptr.To("1"),
			HeadMemory: ptr.To("5Gi"),
			WorkerGroups: []WorkerGroup{
				{
					WorkerReplicas: int32(3),
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
	assert.Equal(t, testRayJobYamlObject.WorkerGroups[0].WorkerReplicas, *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, resource.MustParse(*testRayJobYamlObject.WorkerGroups[0].WorkerCPU), *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse(*testRayJobYamlObject.WorkerGroups[0].WorkerMemory), *result.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Memory())
}

func TestConvertRayClusterApplyConfigToYaml(t *testing.T) {
	testRayClusterConfig := RayClusterConfig{
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
		WorkerGroups: []WorkerGroup{
			{
				WorkerReplicas: int32(3),
				WorkerCPU:      ptr.To("2"),
				WorkerMemory:   ptr.To("10Gi"),
				WorkerGPU:      ptr.To("0"),
			},
		},
	}

	result := testRayClusterConfig.GenerateRayClusterApplyConfig()

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

func TestGenerateResources(t *testing.T) {
	tests := []struct {
		expectedResources corev1.ResourceList
		cpu               *string
		memory            *string
		ephemeralStorage  *string
		gpu               *string
		name              string
	}{
		{
			name:             "should generate resources with CPU, memory, ephemeral storage, and GPU",
			cpu:              ptr.To("1"),
			memory:           ptr.To("5Gi"),
			ephemeralStorage: ptr.To("10Gi"),
			gpu:              ptr.To("1"),
			expectedResources: corev1.ResourceList{
				corev1.ResourceCPU:                          resource.MustParse("1"),
				corev1.ResourceMemory:                       resource.MustParse("5Gi"),
				corev1.ResourceEphemeralStorage:             resource.MustParse("10Gi"),
				corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
			},
		},
		{
			name:             "should only generate resources with CPU and memory if ephemeral storage isn't set and GPUs are 0",
			cpu:              ptr.To("1"),
			memory:           ptr.To("5Gi"),
			ephemeralStorage: ptr.To(""),
			gpu:              ptr.To("0"),
			expectedResources: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("5Gi"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expectedResources, GenerateResources(test.cpu, test.memory, test.ephemeralStorage, test.gpu))
		})
	}
}

func TestGenerateRayClusterSpec(t *testing.T) {
	testRayClusterConfig := RayClusterConfig{
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
		WorkerGroups: []WorkerGroup{
			{
				WorkerReplicas: int32(3),
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
			{
				Name:      ptr.To("worker-group-2"),
				WorkerGPU: ptr.To("1"),
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
									corev1.ResourceCPU:                          resource.MustParse("1"),
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
										corev1.ResourceCPU:    resource.MustParse("2"),
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
			{
				GroupName: ptr.To("worker-group-2"),
				Replicas:  ptr.To(int32(0)),
				RayStartParams: map[string]string{
					"metrics-export-port": "8080",
				},
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					Spec: &corev1ac.PodSpecApplyConfiguration{
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name:  ptr.To("ray-worker"),
								Image: ptr.To("rayproject/ray:1.2.3"),
								Resources: &corev1ac.ResourceRequirementsApplyConfiguration{
									Requests: &corev1.ResourceList{
										corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
									},
									Limits: &corev1.ResourceList{
										corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	result := testRayClusterConfig.generateRayClusterSpec()

	assert.Equal(t, expected, result)
}

func TestSetGCSFuseOptions(t *testing.T) {
	gcsFuse := &GCSFuse{
		BucketName:   "my-bucket",
		MountPath:    "/mnt/my-data",
		MountOptions: ptr.To("uid=1234,gid=5678"),
		Resources: &GCSFuseResources{
			CPU:              ptr.To("1"),
			Memory:           ptr.To("5Gi"),
			EphemeralStorage: ptr.To("10Gi"),
		},
		DisableMetrics:                 ptr.To(false),
		GCSFuseMetadataPrefetchOnMount: ptr.To(true),
		SkipCSIBucketAccessCheck:       ptr.To(true),
	}

	expected := &rayv1ac.RayClusterSpecApplyConfiguration{
		HeadGroupSpec: &rayv1ac.HeadGroupSpecApplyConfiguration{
			Template: &corev1ac.PodTemplateSpecApplyConfiguration{
				ObjectMetaApplyConfiguration: &metav1.ObjectMetaApplyConfiguration{
					Annotations: map[string]string{
						"gke-gcsfuse/cpu-request":               "1",
						"gke-gcsfuse/ephemeral-storage-limit":   "10Gi",
						"gke-gcsfuse/ephemeral-storage-request": "10Gi",
						"gke-gcsfuse/memory-limit":              "5Gi",
						"gke-gcsfuse/memory-request":            "5Gi",
					},
				},
				Spec: &corev1ac.PodSpecApplyConfiguration{
					Containers: []corev1ac.ContainerApplyConfiguration{
						{
							Name: ptr.To("ray-head"),
							VolumeMounts: []corev1ac.VolumeMountApplyConfiguration{
								{
									Name:      ptr.To("cluster-storage"),
									MountPath: ptr.To("/mnt/my-data"),
								},
							},
						},
					},
					Volumes: []corev1ac.VolumeApplyConfiguration{
						{
							Name: ptr.To("cluster-storage"),
							VolumeSourceApplyConfiguration: corev1ac.VolumeSourceApplyConfiguration{
								CSI: &corev1ac.CSIVolumeSourceApplyConfiguration{
									Driver: ptr.To(gcsFuseCSIDriver),
									VolumeAttributes: map[string]string{
										"bucketName":                     "my-bucket",
										"mountOptions":                   "uid=1234,gid=5678",
										"disableMetrics":                 "false",
										"gcsfuseMetadataPrefetchOnMount": "true",
										"skipCSIBucketAccessCheck":       "true",
									},
								},
							},
						},
					},
				},
			},
		},
		WorkerGroupSpecs: []rayv1ac.WorkerGroupSpecApplyConfiguration{
			{
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					ObjectMetaApplyConfiguration: &metav1.ObjectMetaApplyConfiguration{
						Annotations: map[string]string{
							"gke-gcsfuse/cpu-request":               "1",
							"gke-gcsfuse/ephemeral-storage-limit":   "10Gi",
							"gke-gcsfuse/ephemeral-storage-request": "10Gi",
							"gke-gcsfuse/memory-limit":              "5Gi",
							"gke-gcsfuse/memory-request":            "5Gi",
						},
					},
					Spec: &corev1ac.PodSpecApplyConfiguration{
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name: ptr.To("ray-worker"),
								VolumeMounts: []corev1ac.VolumeMountApplyConfiguration{
									{
										Name:      ptr.To("cluster-storage"),
										MountPath: ptr.To("/mnt/my-data"),
									},
								},
							},
						},
						Volumes: []corev1ac.VolumeApplyConfiguration{
							{
								Name: ptr.To("cluster-storage"),
								VolumeSourceApplyConfiguration: corev1ac.VolumeSourceApplyConfiguration{
									CSI: &corev1ac.CSIVolumeSourceApplyConfiguration{
										Driver: ptr.To(gcsFuseCSIDriver),
										VolumeAttributes: map[string]string{
											"bucketName":                     "my-bucket",
											"mountOptions":                   "uid=1234,gid=5678",
											"disableMetrics":                 "false",
											"gcsfuseMetadataPrefetchOnMount": "true",
											"skipCSIBucketAccessCheck":       "true",
										},
									},
								},
							},
						},
					},
				},
			},
			{
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					ObjectMetaApplyConfiguration: &metav1.ObjectMetaApplyConfiguration{
						Annotations: map[string]string{
							"gke-gcsfuse/cpu-request":               "1",
							"gke-gcsfuse/ephemeral-storage-limit":   "10Gi",
							"gke-gcsfuse/ephemeral-storage-request": "10Gi",
							"gke-gcsfuse/memory-limit":              "5Gi",
							"gke-gcsfuse/memory-request":            "5Gi",
						},
					},
					Spec: &corev1ac.PodSpecApplyConfiguration{
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name: ptr.To("ray-worker"),
								VolumeMounts: []corev1ac.VolumeMountApplyConfiguration{
									{
										Name:      ptr.To("cluster-storage"),
										MountPath: ptr.To("/mnt/my-data"),
									},
								},
							},
						},
						Volumes: []corev1ac.VolumeApplyConfiguration{
							{
								Name: ptr.To("cluster-storage"),
								VolumeSourceApplyConfiguration: corev1ac.VolumeSourceApplyConfiguration{
									CSI: &corev1ac.CSIVolumeSourceApplyConfiguration{
										Driver: ptr.To(gcsFuseCSIDriver),
										VolumeAttributes: map[string]string{
											"bucketName":                     "my-bucket",
											"mountOptions":                   "uid=1234,gid=5678",
											"disableMetrics":                 "false",
											"gcsfuseMetadataPrefetchOnMount": "true",
											"skipCSIBucketAccessCheck":       "true",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &rayv1ac.RayClusterSpecApplyConfiguration{
		HeadGroupSpec: &rayv1ac.HeadGroupSpecApplyConfiguration{
			Template: &corev1ac.PodTemplateSpecApplyConfiguration{
				Spec: &corev1ac.PodSpecApplyConfiguration{
					Containers: []corev1ac.ContainerApplyConfiguration{
						{
							Name: ptr.To("ray-head"),
						},
					},
				},
			},
		},
		WorkerGroupSpecs: []rayv1ac.WorkerGroupSpecApplyConfiguration{
			{
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					Spec: &corev1ac.PodSpecApplyConfiguration{
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name: ptr.To("ray-worker"),
							},
						},
					},
				},
			},
			{
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					Spec: &corev1ac.PodSpecApplyConfiguration{
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name: ptr.To("ray-worker"),
							},
						},
					},
				},
			},
		},
	}

	setGCSFuseOptions(result, gcsFuse)

	assert.Equal(t, expected, result)
}

func TestNewRayClusterConfigWithDefaults(t *testing.T) {
	result := newRayClusterConfigWithDefaults()
	expected := &RayClusterConfig{
		Image:      ptr.To(fmt.Sprintf("rayproject/ray:%s", util.RayVersion)),
		RayVersion: ptr.To(util.RayVersion),
		HeadCPU:    ptr.To("2"),
		HeadMemory: ptr.To("4Gi"),
		WorkerGroups: []WorkerGroup{
			{
				Name:           ptr.To("default-group"),
				WorkerCPU:      ptr.To("2"),
				WorkerMemory:   ptr.To("4Gi"),
				WorkerReplicas: int32(1),
			},
		},
	}

	assert.Equal(t, expected, result)
}

func TestParseConfigFile(t *testing.T) {
	tests := map[string]struct {
		config      string
		expected    *RayClusterConfig
		expectedErr string
	}{
		"invalid config": {
			config: `foo: bar`,
			expected: &RayClusterConfig{
				RayVersion: ptr.To(util.RayVersion),
				Image:      ptr.To(fmt.Sprintf("rayproject/ray:%s", util.RayVersion)),
			},
			expectedErr: "field foo not found in type generation.RayClusterConfig",
		},
		"empty config": {
			config: "",
			expected: &RayClusterConfig{
				RayVersion: ptr.To(util.RayVersion),
				Image:      ptr.To(fmt.Sprintf("rayproject/ray:%s", util.RayVersion)),
				HeadCPU:    ptr.To("2"),
				HeadMemory: ptr.To("4Gi"),
				WorkerGroups: []WorkerGroup{
					{
						Name:           ptr.To("default-group"),
						WorkerReplicas: int32(1),
						WorkerCPU:      ptr.To("2"),
						WorkerMemory:   ptr.To("4Gi"),
					},
				},
			},
		},
		"minimal config": {
			config: `worker-groups:
- worker-replicas: 1
  worker-gpu: 1
`,
			expected: &RayClusterConfig{
				RayVersion: ptr.To(util.RayVersion),
				Image:      ptr.To(fmt.Sprintf("rayproject/ray:%s", util.RayVersion)),
				HeadCPU:    ptr.To("2"),
				HeadMemory: ptr.To("4Gi"),
				WorkerGroups: []WorkerGroup{
					{
						WorkerReplicas: int32(1),
						WorkerGPU:      ptr.To("1"),
					},
				},
			},
		},
		"full config": {
			config: `namespace: hyperkube
name: dxia-test

labels:
  foo: bar
annotations:
  dead: beef

ray-version: 2.44.0
image: rayproject/ray:2.44.0

head-cpu: 3
head-memory: 5Gi
head-gpu: 0
head-ephemeral-storage: 8Gi
head-ray-start-params:
  metrics-export-port: 8082

worker-groups:
- name: cpu-workers
  worker-replicas: 1
  worker-cpu: 2
  worker-memory: 4Gi
  worker-gpu: 0
  worker-ephemeral-storage: 12Gi
  worker-ray-start-params:
    metrics-export-port: 8081
- name: gpu-workers
  worker-replicas: 1
  worker-cpu: 3
  worker-memory: 6Gi
  worker-gpu: 1
  worker-ephemeral-storage: 13Gi
  worker-ray-start-params:
    metrics-export-port: 8081

# Cloud Storage FUSE options
gcsfuse:
  bucket-name: my-bucket
  mount-path: /mnt/cluster_storage
  mount-options: "implicit-dirs,uid=1000,gid=100"
  resources:
    cpu: 250m
    memory: 256Mi
    ephemeral-storage: 5Gi
  disable-metrics: true
  gcsfuse-metadata-prefetch-on-mount: false
  skip-csi-bucket-access-check: false
`,
			expected: &RayClusterConfig{
				Namespace:            ptr.To("hyperkube"),
				Name:                 ptr.To("dxia-test"),
				Labels:               map[string]string{"foo": "bar"},
				Annotations:          map[string]string{"dead": "beef"},
				RayVersion:           ptr.To("2.44.0"),
				Image:                ptr.To("rayproject/ray:2.44.0"),
				HeadCPU:              ptr.To("3"),
				HeadMemory:           ptr.To("5Gi"),
				HeadGPU:              ptr.To("0"),
				HeadRayStartParams:   map[string]string{"metrics-export-port": "8082"},
				HeadEphemeralStorage: ptr.To("8Gi"),
				WorkerGroups: []WorkerGroup{
					{
						Name:                   ptr.To("cpu-workers"),
						WorkerReplicas:         int32(1),
						WorkerCPU:              ptr.To("2"),
						WorkerMemory:           ptr.To("4Gi"),
						WorkerGPU:              ptr.To("0"),
						WorkerEphemeralStorage: ptr.To("12Gi"),
						WorkerRayStartParams:   map[string]string{"metrics-export-port": "8081"},
					},
					{
						Name:                   ptr.To("gpu-workers"),
						WorkerReplicas:         int32(1),
						WorkerCPU:              ptr.To("3"),
						WorkerMemory:           ptr.To("6Gi"),
						WorkerGPU:              ptr.To("1"),
						WorkerEphemeralStorage: ptr.To("13Gi"),
						WorkerRayStartParams:   map[string]string{"metrics-export-port": "8081"},
					},
				},
				GCSFuse: &GCSFuse{
					BucketName:   "my-bucket",
					MountPath:    "/mnt/cluster_storage",
					MountOptions: ptr.To("implicit-dirs,uid=1000,gid=100"),
					Resources: &GCSFuseResources{
						CPU:              ptr.To("250m"),
						Memory:           ptr.To("256Mi"),
						EphemeralStorage: ptr.To("5Gi"),
					},
					DisableMetrics:                 ptr.To(true),
					GCSFuseMetadataPrefetchOnMount: ptr.To(false),
					SkipCSIBucketAccessCheck:       ptr.To(false),
				},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "test-config.yaml")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(test.config)
			require.NoError(t, err)

			result, err := ParseConfigFile(tmpFile.Name())
			if test.expectedErr != "" {
				assert.Contains(t, err.Error(), test.expectedErr)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, test.expected, result)
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := map[string]struct {
		config      *RayClusterConfig
		expectedErr string
	}{
		"invalid config": {
			config: &RayClusterConfig{
				HeadCPU: ptr.To("invalid"),
			},
			expectedErr: "head-cpu is not a valid resource quantity",
		},
		"valid config": {
			config: &RayClusterConfig{
				HeadCPU:              ptr.To("2"),
				HeadMemory:           ptr.To("4Gi"),
				HeadGPU:              ptr.To("0"),
				HeadEphemeralStorage: ptr.To("8Gi"),
				WorkerGroups: []WorkerGroup{
					{
						WorkerCPU:              ptr.To("2"),
						WorkerMemory:           ptr.To("4Gi"),
						WorkerGPU:              ptr.To("0"),
						WorkerEphemeralStorage: ptr.To("8Gi"),
					},
				},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateConfig(test.config)
			if test.expectedErr != "" {
				assert.Contains(t, err.Error(), test.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateGCSFuse(t *testing.T) {
	tests := map[string]struct {
		config      *GCSFuse
		expectedErr string
	}{
		"bucket name is required": {
			config:      &GCSFuse{},
			expectedErr: ".gcsfuse.bucket-name is required",
		},
		"mount path is required": {
			config: &GCSFuse{
				BucketName: "my-bucket",
			},
			expectedErr: ".gcsfuse.mount-path is required",
		},
		"valid config": {
			config: &GCSFuse{
				BucketName: "my-bucket",
				MountPath:  "/mnt/cluster_storage",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateGCSFuse(test.config)
			if test.expectedErr != "" {
				assert.Contains(t, err.Error(), test.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetGCSFuseVolumeAttributes(t *testing.T) {
	config := &GCSFuse{
		BucketName:                     "my-bucket",
		MountPath:                      "/mnt/cluster_storage",
		MountOptions:                   ptr.To("implicit-dirs,uid=1000,gid=100"),
		DisableMetrics:                 ptr.To(true),
		GCSFuseMetadataPrefetchOnMount: ptr.To(false),
		SkipCSIBucketAccessCheck:       ptr.To(false),
	}

	expected := map[string]string{
		"bucketName":                     "my-bucket",
		"mountOptions":                   "implicit-dirs,uid=1000,gid=100",
		"disableMetrics":                 "true",
		"gcsfuseMetadataPrefetchOnMount": "false",
		"skipCSIBucketAccessCheck":       "false",
	}

	result := getGCSFuseVolumeAttributes(config)
	assert.Equal(t, expected, result)
}
