package generation

import (
	"fmt"
	"os"
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
		Head: &Head{
			CPU:    ptr.To("1"),
			Memory: ptr.To("5Gi"),
			GPU:    ptr.To("1"),
			RayStartParams: map[string]string{
				"dashboard-host": "1.2.3.4",
				"num-cpus":       "0",
			},
		},
		WorkerGroups: []WorkerGroup{
			{
				Replicas:   int32(3),
				NumOfHosts: ptr.To(int32(2)),
				CPU:        ptr.To("2"),
				Memory:     ptr.To("10Gi"),
				GPU:        ptr.To("1"),
				TPU:        ptr.To("1"),
				RayStartParams: map[string]string{
					"dagon":    "azathoth",
					"shoggoth": "cthulhu",
				},
				NodeSelectors: map[string]string{
					"app": "ray",
					"env": "dev",
				},
			},
		},
		Autoscaler: &Autoscaler{
			Version: AutoscalerV2,
		},
	}

	result := testRayClusterConfig.GenerateRayClusterApplyConfig()

	expected := rayv1ac.RayClusterApplyConfiguration{
		TypeMetaApplyConfiguration: metav1.TypeMetaApplyConfiguration{
			APIVersion: ptr.To("ray.io/v1"),
			Kind:       ptr.To("RayCluster"),
		},
		ObjectMetaApplyConfiguration: &metav1.ObjectMetaApplyConfiguration{
			Name:        ptr.To("test-ray-cluster"),
			Namespace:   ptr.To("default"),
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: &rayv1ac.RayClusterSpecApplyConfiguration{
			EnableInTreeAutoscaling: ptr.To(true),
			AutoscalerOptions: &rayv1ac.AutoscalerOptionsApplyConfiguration{
				Version: ptr.To(rayv1.AutoscalerVersionV2),
			},
			RayVersion: ptr.To(util.RayVersion),
			HeadGroupSpec: &rayv1ac.HeadGroupSpecApplyConfiguration{
				RayStartParams: map[string]string{"dashboard-host": "1.2.3.4", "num-cpus": "0"},
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					Spec: &corev1ac.PodSpecApplyConfiguration{
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name:  ptr.To("ray-head"),
								Image: ptr.To(util.RayImage),
								Resources: &corev1ac.ResourceRequirementsApplyConfiguration{
									Requests: &corev1.ResourceList{
										corev1.ResourceCPU:                          resource.MustParse("1"),
										corev1.ResourceMemory:                       resource.MustParse("5Gi"),
										corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
									},
									Limits: &corev1.ResourceList{
										corev1.ResourceMemory:                       resource.MustParse("5Gi"),
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
					},
				},
			},
			WorkerGroupSpecs: []rayv1ac.WorkerGroupSpecApplyConfiguration{
				{
					GroupName:      ptr.To("default-group"),
					Replicas:       ptr.To(int32(3)),
					NumOfHosts:     ptr.To(int32(2)),
					RayStartParams: map[string]string{"dagon": "azathoth", "shoggoth": "cthulhu"},
					Template: &corev1ac.PodTemplateSpecApplyConfiguration{
						Spec: &corev1ac.PodSpecApplyConfiguration{
							Containers: []corev1ac.ContainerApplyConfiguration{
								{
									Name:  ptr.To("ray-worker"),
									Image: ptr.To(util.RayImage),
									Resources: &corev1ac.ResourceRequirementsApplyConfiguration{
										Requests: &corev1.ResourceList{
											corev1.ResourceCPU:                          resource.MustParse("2"),
											corev1.ResourceMemory:                       resource.MustParse("10Gi"),
											corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
											corev1.ResourceName(util.ResourceGoogleTPU): resource.MustParse("1"),
										},
										Limits: &corev1.ResourceList{
											corev1.ResourceMemory:                       resource.MustParse("10Gi"),
											corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
											corev1.ResourceName(util.ResourceGoogleTPU): resource.MustParse("1"),
										},
									},
								},
							},
							NodeSelector: map[string]string{"app": "ray", "env": "dev"},
						},
					},
				},
			},
		},
	}

	require.Equal(t, &expected, result)
}

func TestGenerateRayJobApplyConfig(t *testing.T) {
	testRayJobYamlObject := RayJobYamlObject{
		RayJobName:               "test-ray-job",
		Namespace:                "default",
		SubmissionMode:           "InteractiveMode",
		TTLSecondsAfterFinished:  100,
		ShutdownAfterJobFinishes: true,
		RayClusterConfig: RayClusterConfig{
			RayVersion: ptr.To(util.RayVersion),
			Image:      ptr.To(util.RayImage),
			Head: &Head{
				CPU:    ptr.To("1"),
				GPU:    ptr.To("1"),
				Memory: ptr.To("5Gi"),
			},
			WorkerGroups: []WorkerGroup{
				{
					Replicas:   int32(3),
					NumOfHosts: ptr.To(int32(2)),
					CPU:        ptr.To("2"),
					Memory:     ptr.To("10Gi"),
					GPU:        ptr.To("0"),
					TPU:        ptr.To("0"),
					RayStartParams: map[string]string{
						"dagon":    "azathoth",
						"shoggoth": "cthulhu",
					},
				},
			},
		},
	}

	result := testRayJobYamlObject.GenerateRayJobApplyConfig()

	expected := rayv1ac.RayJobApplyConfiguration{
		TypeMetaApplyConfiguration: metav1.TypeMetaApplyConfiguration{
			APIVersion: ptr.To("ray.io/v1"),
			Kind:       ptr.To("RayJob"),
		},
		ObjectMetaApplyConfiguration: &metav1.ObjectMetaApplyConfiguration{
			Name:      ptr.To("test-ray-job"),
			Namespace: ptr.To("default"),
		},
		Spec: &rayv1ac.RayJobSpecApplyConfiguration{
			SubmissionMode:           ptr.To(rayv1.JobSubmissionMode(testRayJobYamlObject.SubmissionMode)),
			Entrypoint:               ptr.To(""),
			TTLSecondsAfterFinished:  ptr.To(int32(100)),
			ShutdownAfterJobFinishes: ptr.To(true),
			RayClusterSpec: &rayv1ac.RayClusterSpecApplyConfiguration{
				RayVersion: ptr.To(util.RayVersion),
				HeadGroupSpec: &rayv1ac.HeadGroupSpecApplyConfiguration{
					Template: &corev1ac.PodTemplateSpecApplyConfiguration{
						Spec: &corev1ac.PodSpecApplyConfiguration{
							Containers: []corev1ac.ContainerApplyConfiguration{
								{
									Name:  ptr.To("ray-head"),
									Image: ptr.To(util.RayImage),
									Resources: &corev1ac.ResourceRequirementsApplyConfiguration{
										Requests: &corev1.ResourceList{
											corev1.ResourceCPU:                          resource.MustParse("1"),
											corev1.ResourceMemory:                       resource.MustParse("5Gi"),
											corev1.ResourceName(util.ResourceNvidiaGPU): resource.MustParse("1"),
										},
										Limits: &corev1.ResourceList{
											corev1.ResourceMemory:                       resource.MustParse("5Gi"),
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
						},
					},
				},
				WorkerGroupSpecs: []rayv1ac.WorkerGroupSpecApplyConfiguration{
					{
						GroupName:      ptr.To("default-group"),
						Replicas:       ptr.To(int32(3)),
						NumOfHosts:     ptr.To(int32(2)),
						RayStartParams: map[string]string{"dagon": "azathoth", "shoggoth": "cthulhu"},
						Template: &corev1ac.PodTemplateSpecApplyConfiguration{
							Spec: &corev1ac.PodSpecApplyConfiguration{
								Containers: []corev1ac.ContainerApplyConfiguration{
									{
										Name:  ptr.To("ray-worker"),
										Image: ptr.To(util.RayImage),
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
							},
						},
					},
				},
			},
		},
	}

	require.Equal(t, &expected, result)
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
		Autoscaler: &Autoscaler{
			Version: AutoscalerV1,
		},
		RayVersion: ptr.To(util.RayVersion),
		Image:      ptr.To(util.RayImage),
		Head: &Head{
			CPU:    ptr.To("1"),
			Memory: ptr.To("5Gi"),
			GPU:    ptr.To("1"),
			RayStartParams: map[string]string{
				"num-cpus": "0",
			},
		},
		WorkerGroups: []WorkerGroup{
			{
				Replicas:   int32(3),
				NumOfHosts: ptr.To(int32(2)),
				CPU:        ptr.To("2"),
				Memory:     ptr.To("10Gi"),
				GPU:        ptr.To("0"),
				TPU:        ptr.To("0"),
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
  enableInTreeAutoscaling: true
  autoscalerOptions:
    version: v1
  headGroupSpec:
    rayStartParams:
      num-cpus: "0"
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
    replicas: 3
    numOfHosts: 2
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

	assert.YAMLEq(t, expectedResultYaml, resultString)
}

func TestGenerateResources(t *testing.T) {
	tests := []struct {
		expectedRequestResources corev1.ResourceList
		expectedLimitResources   corev1.ResourceList
		cpu                      *string
		memory                   *string
		ephemeralStorage         *string
		gpu                      *string
		tpu                      *string
		name                     string
	}{
		{
			name:             "should generate resources with CPU, memory, ephemeral storage, and GPU",
			cpu:              ptr.To("1"),
			memory:           ptr.To("5Gi"),
			ephemeralStorage: ptr.To("10Gi"),
			gpu:              ptr.To("1"),
			tpu:              ptr.To("0"),
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
		{
			name:             "should generate resources with CPU, memory, ephemeral storage, and TPU",
			cpu:              ptr.To("1"),
			memory:           ptr.To("5Gi"),
			ephemeralStorage: ptr.To("10Gi"),
			gpu:              ptr.To("0"),
			tpu:              ptr.To("4"),
			expectedRequestResources: corev1.ResourceList{
				corev1.ResourceCPU:                          resource.MustParse("1"),
				corev1.ResourceMemory:                       resource.MustParse("5Gi"),
				corev1.ResourceEphemeralStorage:             resource.MustParse("10Gi"),
				corev1.ResourceName(util.ResourceGoogleTPU): resource.MustParse("4"),
			},
			expectedLimitResources: corev1.ResourceList{
				corev1.ResourceMemory:                       resource.MustParse("5Gi"),
				corev1.ResourceEphemeralStorage:             resource.MustParse("10Gi"),
				corev1.ResourceName(util.ResourceGoogleTPU): resource.MustParse("4"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expectedRequestResources, generateRequestResources(test.cpu, test.memory, test.ephemeralStorage, test.gpu, test.tpu))
			assert.Equal(t, test.expectedLimitResources, generateLimitResources(test.memory, test.ephemeralStorage, test.gpu, test.tpu))
		})
	}
}

func TestGenerateRayClusterSpec(t *testing.T) {
	testRayClusterConfig := RayClusterConfig{
		Autoscaler: &Autoscaler{
			Version: AutoscalerV2,
		},
		RayVersion:     ptr.To("1.2.3"),
		Image:          ptr.To("rayproject/ray:2.46.0"),
		ServiceAccount: ptr.To("my-service-account"),
		Head: &Head{
			CPU:              ptr.To("1"),
			Memory:           ptr.To("5Gi"),
			GPU:              ptr.To("1"),
			EphemeralStorage: ptr.To("10Gi"),
			RayStartParams: map[string]string{
				"softmax": "GELU",
			},
			NodeSelectors: map[string]string{
				"head-selector1": "foo",
				"head-selector2": "bar",
			},
		},
		WorkerGroups: []WorkerGroup{
			{
				Replicas:   int32(3),
				NumOfHosts: ptr.To(int32(1)),
				CPU:        ptr.To("2"),
				Memory:     ptr.To("10Gi"),
				GPU:        ptr.To("0"),
				TPU:        ptr.To("0"),
				RayStartParams: map[string]string{
					"dagon":    "azathoth",
					"shoggoth": "cthulhu",
				},
				NodeSelectors: map[string]string{
					"worker-selector1": "baz",
					"worker-selector2": "qux",
				},
			},
			{
				Name: ptr.To("worker-group-2"),
				GPU:  ptr.To("1"),
			},
		},
	}

	expected := &rayv1ac.RayClusterSpecApplyConfiguration{
		EnableInTreeAutoscaling: ptr.To(true),
		AutoscalerOptions: &rayv1ac.AutoscalerOptionsApplyConfiguration{
			Version: ptr.To(rayv1.AutoscalerVersionV2),
		},
		RayVersion: ptr.To("1.2.3"),
		HeadGroupSpec: &rayv1ac.HeadGroupSpecApplyConfiguration{
			RayStartParams: map[string]string{"softmax": "GELU"},
			Template: &corev1ac.PodTemplateSpecApplyConfiguration{
				Spec: &corev1ac.PodSpecApplyConfiguration{
					ServiceAccountName: ptr.To("my-service-account"),
					Containers: []corev1ac.ContainerApplyConfiguration{
						{
							Name:  ptr.To("ray-head"),
							Image: ptr.To("rayproject/ray:2.46.0"),
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
				GroupName:  ptr.To("default-group"),
				NumOfHosts: ptr.To(int32(1)),
				Replicas:   ptr.To(int32(3)),
				RayStartParams: map[string]string{
					"dagon":    "azathoth",
					"shoggoth": "cthulhu",
				},
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					Spec: &corev1ac.PodSpecApplyConfiguration{
						ServiceAccountName: ptr.To("my-service-account"),
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name:  ptr.To("ray-worker"),
								Image: ptr.To("rayproject/ray:2.46.0"),
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
			{
				GroupName: ptr.To("worker-group-2"),
				Replicas:  ptr.To(int32(0)),
				Template: &corev1ac.PodTemplateSpecApplyConfiguration{
					Spec: &corev1ac.PodSpecApplyConfiguration{
						ServiceAccountName: ptr.To("my-service-account"),
						Containers: []corev1ac.ContainerApplyConfiguration{
							{
								Name:  ptr.To("ray-worker"),
								Image: ptr.To("rayproject/ray:2.46.0"),
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
		Head: &Head{
			CPU:    ptr.To("2"),
			Memory: ptr.To("4Gi"),
		},
		WorkerGroups: []WorkerGroup{
			{
				Name:     ptr.To("default-group"),
				CPU:      ptr.To("2"),
				Memory:   ptr.To("4Gi"),
				Replicas: int32(1),
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
				Head: &Head{
					CPU:    ptr.To("2"),
					Memory: ptr.To("4Gi"),
				},
				WorkerGroups: []WorkerGroup{
					{
						Name:     ptr.To("default-group"),
						Replicas: int32(1),
						CPU:      ptr.To("2"),
						Memory:   ptr.To("4Gi"),
					},
				},
			},
		},
		"minimal config": {
			config: `worker-groups:
- replicas: 1
  gpu: 1
`,
			expected: &RayClusterConfig{
				RayVersion: ptr.To(util.RayVersion),
				Image:      ptr.To(fmt.Sprintf("rayproject/ray:%s", util.RayVersion)),
				Head: &Head{
					CPU:    ptr.To("2"),
					Memory: ptr.To("4Gi"),
				},
				WorkerGroups: []WorkerGroup{
					{
						Name:     ptr.To("default-group"),
						Replicas: int32(1),
						CPU:      ptr.To("2"),
						GPU:      ptr.To("1"),
						Memory:   ptr.To("4Gi"),
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

ray-version: 2.46.0
image: rayproject/ray:2.46.0

head:
  cpu: 3
  memory: 5Gi
  gpu: 0
  ephemeral-storage: 8Gi
  ray-start-params:
    metrics-export-port: 8082

worker-groups:
- name: cpu-workers
  replicas: 1
  cpu: 2
  memory: 4Gi
  gpu: 0
  ephemeral-storage: 12Gi
  ray-start-params:
    metrics-export-port: 8081
- name: gpu-workers
  replicas: 1
  cpu: 3
  memory: 6Gi
  gpu: 1
  ephemeral-storage: 13Gi
  ray-start-params:
    metrics-export-port: 8081

gke:
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
				Namespace:   ptr.To("hyperkube"),
				Name:        ptr.To("dxia-test"),
				Labels:      map[string]string{"foo": "bar"},
				Annotations: map[string]string{"dead": "beef"},
				RayVersion:  ptr.To("2.46.0"),
				Image:       ptr.To("rayproject/ray:2.46.0"),
				Head: &Head{
					CPU:              ptr.To("3"),
					Memory:           ptr.To("5Gi"),
					GPU:              ptr.To("0"),
					RayStartParams:   map[string]string{"metrics-export-port": "8082"},
					EphemeralStorage: ptr.To("8Gi"),
				},
				WorkerGroups: []WorkerGroup{
					{
						Name:             ptr.To("cpu-workers"),
						Replicas:         int32(1),
						CPU:              ptr.To("2"),
						Memory:           ptr.To("4Gi"),
						GPU:              ptr.To("0"),
						EphemeralStorage: ptr.To("12Gi"),
						RayStartParams:   map[string]string{"metrics-export-port": "8081"},
					},
					{
						Name:             ptr.To("gpu-workers"),
						Replicas:         int32(1),
						CPU:              ptr.To("3"),
						Memory:           ptr.To("6Gi"),
						GPU:              ptr.To("1"),
						EphemeralStorage: ptr.To("13Gi"),
						RayStartParams:   map[string]string{"metrics-export-port": "8081"},
					},
				},
				GKE: &GKE{
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
				Head: &Head{
					CPU: ptr.To("invalid"),
				},
			},
			expectedErr: "cpu is not a valid resource quantity",
		},
		"valid config": {
			config: &RayClusterConfig{
				Head: &Head{
					CPU:              ptr.To("2"),
					Memory:           ptr.To("4Gi"),
					GPU:              ptr.To("0"),
					EphemeralStorage: ptr.To("8Gi"),
				},
				WorkerGroups: []WorkerGroup{
					{
						CPU:              ptr.To("2"),
						Memory:           ptr.To("4Gi"),
						GPU:              ptr.To("0"),
						EphemeralStorage: ptr.To("8Gi"),
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

func TestMergeWithDefaults(t *testing.T) {
	defaultRayVersion := util.RayVersion
	defaultImage := fmt.Sprintf("rayproject/ray:%s", util.RayVersion)

	t.Run("Empty RayClusterConfig and return default RayClusterConfig", func(t *testing.T) {
		result, err := mergeWithDefaultConfig(&RayClusterConfig{})
		require.NoError(t, err)
		assert.NotNil(t, result)
		expected := newRayClusterConfigWithDefaults()
		assert.Equal(t, expected, result)
	})

	t.Run("Override namespace, name, labels, annotations", func(t *testing.T) {
		inputNamespace := ptr.To("test-namespace")
		inputName := ptr.To("test-name")
		inputLabels := map[string]string{"key1": "value1", "key2": "value2"}
		inputAnnotations := map[string]string{"annotation1": "value1", "annotation2": "value2"}

		override := &RayClusterConfig{
			Namespace:   inputNamespace,
			Name:        inputName,
			Labels:      inputLabels,
			Annotations: inputAnnotations,
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, inputNamespace, result.Namespace)
		assert.Equal(t, inputName, result.Name)
		assert.Equal(t, inputLabels, result.Labels)
		assert.Equal(t, inputAnnotations, result.Annotations)
	})

	t.Run("Override RayVersion, Image, ServiceAccount", func(t *testing.T) {
		inputRayVersion := ptr.To("4.16.0")
		inputImage := ptr.To("custom/image:tag")
		inputServiceAccount := ptr.To("svcacct")

		override := &RayClusterConfig{
			RayVersion:     inputRayVersion,
			Image:          inputImage,
			ServiceAccount: inputServiceAccount,
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, inputRayVersion, result.RayVersion)
		assert.Equal(t, inputImage, result.Image)
		assert.Equal(t, inputServiceAccount, result.ServiceAccount)
	})

	t.Run("Override Head fields", func(t *testing.T) {
		headCPU := ptr.To("4")
		headGPU := ptr.To("2")
		headMemory := ptr.To("8Gi")
		headEphemeralStorage := ptr.To("20Gi")
		headRayStartParams := map[string]string{"foo": "bar"}
		headNodeSelectors := map[string]string{"disktype": "ssd"}

		override := &RayClusterConfig{
			Head: &Head{
				CPU:              headCPU,
				GPU:              headGPU,
				Memory:           headMemory,
				EphemeralStorage: headEphemeralStorage,
				RayStartParams:   headRayStartParams,
				NodeSelectors:    headNodeSelectors,
			},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, headCPU, result.Head.CPU)
		assert.Equal(t, headGPU, result.Head.GPU)
		assert.Equal(t, headMemory, result.Head.Memory)
		assert.Equal(t, headEphemeralStorage, result.Head.EphemeralStorage)
		assert.Equal(t, headRayStartParams, result.Head.RayStartParams)
		assert.Equal(t, headNodeSelectors, result.Head.NodeSelectors)
	})

	t.Run("Override only some fields in Head, others remain default", func(t *testing.T) {
		headCPU := ptr.To("8")

		override := &RayClusterConfig{
			Head: &Head{
				CPU: headCPU,
			},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, headCPU, result.Head.CPU)
		assert.Equal(t, ptr.To("4Gi"), result.Head.Memory)
		assert.Equal(t, defaultRayVersion, *result.RayVersion)
		assert.Equal(t, defaultImage, *result.Image)
	})

	t.Run("Override GKE.GCSFuse fields", func(t *testing.T) {
		gcsFuseMountOption := ptr.To("opt1")
		gcsFuseDisableMetrics := ptr.To(true)
		gcsFuseMetadataPrefetchOnMount := ptr.To(true)
		gcsFuseSkipCSIBucketAccessCheck := ptr.To(true)
		gcsFuseBucketName := "bucket"
		gcsFuseMountPath := "/mnt/path"
		gcsFuseCPU := ptr.To("1")
		gcsFuseMemory := ptr.To("2Gi")
		gcsFuseEphemeralStorage := ptr.To("3Gi")
		gcsFuseResources := &GCSFuseResources{
			CPU:              gcsFuseCPU,
			Memory:           gcsFuseMemory,
			EphemeralStorage: gcsFuseEphemeralStorage,
		}

		override := &RayClusterConfig{
			GKE: &GKE{
				GCSFuse: &GCSFuse{
					MountOptions:                   gcsFuseMountOption,
					DisableMetrics:                 gcsFuseDisableMetrics,
					GCSFuseMetadataPrefetchOnMount: gcsFuseMetadataPrefetchOnMount,
					SkipCSIBucketAccessCheck:       gcsFuseSkipCSIBucketAccessCheck,
					BucketName:                     gcsFuseBucketName,
					MountPath:                      gcsFuseMountPath,
					Resources: &GCSFuseResources{
						CPU:              gcsFuseCPU,
						Memory:           gcsFuseMemory,
						EphemeralStorage: gcsFuseEphemeralStorage,
					},
				},
			},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.GKE)
		assert.NotNil(t, result.GKE.GCSFuse)
		assert.Equal(t, gcsFuseMountOption, result.GKE.GCSFuse.MountOptions)
		assert.Equal(t, gcsFuseDisableMetrics, result.GKE.GCSFuse.DisableMetrics)
		assert.Equal(t, gcsFuseMetadataPrefetchOnMount, result.GKE.GCSFuse.GCSFuseMetadataPrefetchOnMount)
		assert.Equal(t, gcsFuseSkipCSIBucketAccessCheck, result.GKE.GCSFuse.SkipCSIBucketAccessCheck)
		assert.Equal(t, gcsFuseBucketName, result.GKE.GCSFuse.BucketName)
		assert.Equal(t, gcsFuseMountPath, result.GKE.GCSFuse.MountPath)
		assert.Equal(t, gcsFuseResources, result.GKE.GCSFuse.Resources)
		assert.Equal(t, gcsFuseCPU, result.GKE.GCSFuse.Resources.CPU)
		assert.Equal(t, gcsFuseMemory, result.GKE.GCSFuse.Resources.Memory)
		assert.Equal(t, gcsFuseEphemeralStorage, result.GKE.GCSFuse.Resources.EphemeralStorage)
	})

	t.Run("Override Autoscaler", func(t *testing.T) {
		override := &RayClusterConfig{
			Autoscaler: &Autoscaler{Version: AutoscalerV2},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Autoscaler)
		assert.Equal(t, AutoscalerV2, result.Autoscaler.Version)
	})

	t.Run("Override WorkerGroups fields", func(t *testing.T) {
		wgName1 := ptr.To("wg1")
		wgCPU := ptr.To("5")
		wgGPU := ptr.To("1")
		wgTPU := ptr.To("2")
		wgNumOfHosts := ptr.To(int32(3))
		wgMemory := ptr.To("16Gi")
		wgEphemeralStorage := ptr.To("30Gi")
		wgRayStartParams := map[string]string{"param": "val"}
		wgNodeSelectors := map[string]string{"zone": "us-central1-a"}
		wgReplicas := int32(7)

		override := &RayClusterConfig{
			WorkerGroups: []WorkerGroup{
				{
					Name:             wgName1,
					CPU:              wgCPU,
					GPU:              wgGPU,
					TPU:              wgTPU,
					NumOfHosts:       wgNumOfHosts,
					Memory:           wgMemory,
					EphemeralStorage: wgEphemeralStorage,
					RayStartParams:   wgRayStartParams,
					NodeSelectors:    wgNodeSelectors,
					Replicas:         wgReplicas,
				},
			},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		require.Len(t, result.WorkerGroups, 1)
		wg := result.WorkerGroups[0]
		assert.Equal(t, wgName1, wg.Name)
		assert.Equal(t, wgCPU, wg.CPU)
		assert.Equal(t, wgGPU, wg.GPU)
		assert.Equal(t, wgTPU, wg.TPU)
		assert.Equal(t, wgNumOfHosts, wg.NumOfHosts)
		assert.Equal(t, wgMemory, wg.Memory)
		assert.Equal(t, wgEphemeralStorage, wg.EphemeralStorage)
		assert.Equal(t, wgRayStartParams, wg.RayStartParams)
		assert.Equal(t, wgNodeSelectors, wg.NodeSelectors)
		assert.Equal(t, wgReplicas, wg.Replicas)
	})

	t.Run("Override WorkerGroups with more groups than defaults", func(t *testing.T) {
		wg1Name := ptr.To("wg1")
		wg2Name := ptr.To("wg2")
		wg1Replicas := int32(2)
		wg2Replicas := int32(3)

		override := &RayClusterConfig{
			WorkerGroups: []WorkerGroup{
				{Name: wg1Name, Replicas: wg1Replicas},
				{Name: wg2Name, Replicas: wg2Replicas},
			},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		require.Len(t, result.WorkerGroups, 2)
		assert.Equal(t, wg1Name, result.WorkerGroups[0].Name)
		assert.Equal(t, wg1Replicas, result.WorkerGroups[0].Replicas)
		assert.Equal(t, wg2Name, result.WorkerGroups[1].Name)
		assert.Equal(t, wg2Replicas, result.WorkerGroups[1].Replicas)
	})

	t.Run("Override WorkerGroups with zero replicas keeps default", func(t *testing.T) {
		wg1Name := ptr.To("wg1")

		override := &RayClusterConfig{
			WorkerGroups: []WorkerGroup{
				{Name: wg1Name, Replicas: 0},
			},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		require.Len(t, result.WorkerGroups, 1)
		assert.Equal(t, wg1Name, result.WorkerGroups[0].Name)
		assert.Equal(t, int32(1), result.WorkerGroups[0].Replicas)
	})

	t.Run("Override WorkerGroups with empty name keeps default name", func(t *testing.T) {
		override := &RayClusterConfig{
			WorkerGroups: []WorkerGroup{
				{Name: nil, Replicas: 2},
			},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		require.Len(t, result.WorkerGroups, 1)
		assert.Equal(t, result.WorkerGroups[0].Name, ptr.To("default-group"))
		assert.Equal(t, int32(2), result.WorkerGroups[0].Replicas)
	})

	t.Run("Override only WorkerGroups CPU", func(t *testing.T) {
		override := &RayClusterConfig{
			WorkerGroups: []WorkerGroup{
				{CPU: ptr.To("1")},
			},
		}
		result, err := mergeWithDefaultConfig(override)
		require.NoError(t, err)
		assert.NotNil(t, result)
		require.Len(t, result.WorkerGroups, 1)
		assert.Equal(t, result.WorkerGroups[0].Name, ptr.To("default-group"))
		assert.Equal(t, int32(1), result.WorkerGroups[0].Replicas)
		assert.Equal(t, ptr.To("1"), result.WorkerGroups[0].CPU)
		assert.Equal(t, ptr.To("4Gi"), result.WorkerGroups[0].Memory)
	})
}
