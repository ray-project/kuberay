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
						Replicas: int32(1),
						GPU:      ptr.To("1"),
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
