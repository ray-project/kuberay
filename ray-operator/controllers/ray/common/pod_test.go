package common

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

var testMemoryLimit = resource.MustParse("1Gi")

var instance = rayv1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayv1.RayClusterSpec{
		HeadGroupSpec: rayv1.HeadGroupSpec{
			RayStartParams: map[string]string{},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "ray-head",
							Image: "repo/image:custom",
							Env: []corev1.EnvVar{
								{
									Name:  "TEST_ENV_NAME",
									Value: "TEST_ENV_VALUE",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: testMemoryLimit,
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: testMemoryLimit,
								},
							},
						},
					},
				},
			},
		},
		WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
			{
				Replicas:    ptr.To[int32](3),
				MinReplicas: ptr.To[int32](0),
				MaxReplicas: ptr.To[int32](10000),
				GroupName:   "small-group",
				RayStartParams: map[string]string{
					"port": "6379",
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-worker",
								Image: "repo/image:custom",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: testMemoryLimit,
										"nvidia.com/gpu":      resource.MustParse("3"),
									},
								},
								Env: []corev1.EnvVar{
									{
										Name:  "TEST_ENV_NAME",
										Value: "TEST_ENV_VALUE",
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

var volumesNoAutoscaler = []corev1.Volume{
	{
		Name: "shared-mem",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: &testMemoryLimit,
			},
		},
	},
}

var volumesWithAutoscaler = []corev1.Volume{
	{
		Name: "shared-mem",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: &testMemoryLimit,
			},
		},
	},
	{
		Name: "ray-logs",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: corev1.StorageMediumDefault,
			},
		},
	},
}

var volumeMountsNoAutoscaler = []corev1.VolumeMount{
	{
		Name:      "shared-mem",
		MountPath: "/dev/shm",
		ReadOnly:  false,
	},
}

var volumeMountsWithAutoscaler = []corev1.VolumeMount{
	{
		Name:      "shared-mem",
		MountPath: "/dev/shm",
		ReadOnly:  false,
	},
	{
		Name:      "ray-logs",
		MountPath: "/tmp/ray",
		ReadOnly:  false,
	},
}

var autoscalerContainer = corev1.Container{
	Name:            "autoscaler",
	Image:           "repo/image:custom",
	ImagePullPolicy: corev1.PullIfNotPresent,
	Env: []corev1.EnvVar{
		{
			Name: utils.RAY_CLUSTER_NAME,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.labels['%s']", utils.RayClusterLabelKey),
				},
			},
		},
		{
			Name: "RAY_CLUSTER_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name: "RAY_HEAD_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name:  "KUBERAY_CRD_VER",
			Value: "v1",
		},
	},
	Command: []string{
		"/bin/bash",
		"-lc",
		"--",
	},
	Args: []string{
		"ray kuberay-autoscaler --cluster-name $(RAY_CLUSTER_NAME) --cluster-namespace $(RAY_CLUSTER_NAMESPACE)",
	},
	Resources: corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	},
	VolumeMounts: []corev1.VolumeMount{
		{
			MountPath: "/tmp/ray",
			Name:      "ray-logs",
		},
	},
}

var trueFlag = true

func TestAddEmptyDirVolumes(t *testing.T) {
	testPod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "ray-worker",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared-mem",
							MountPath: "/dev/shm",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "shared-mem",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							Medium: corev1.StorageMediumMemory,
						},
					},
				},
			},
		},
	}
	assert.Len(t, testPod.Spec.Containers[0].VolumeMounts, 1)
	assert.Len(t, testPod.Spec.Volumes, 1)
	addEmptyDir(context.Background(), &testPod.Spec.Containers[0], testPod, "shared-mem2", "/dev/shm2", corev1.StorageMediumDefault)
	assert.Len(t, testPod.Spec.Containers[0].VolumeMounts, 2)
	assert.Len(t, testPod.Spec.Volumes, 2)
	addEmptyDir(context.Background(), &testPod.Spec.Containers[0], testPod, "shared-mem2", "/dev/shm2", corev1.StorageMediumDefault)
	assert.Len(t, testPod.Spec.Containers[0].VolumeMounts, 2)
	assert.Len(t, testPod.Spec.Volumes, 2)
}

func TestGetHeadPort(t *testing.T) {
	headStartParams := make(map[string]string)
	actualResult := GetHeadPort(headStartParams)
	expectedResult := "6379"
	if !(actualResult == expectedResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	headStartParams["port"] = "9999"
	actualResult = GetHeadPort(headStartParams)
	expectedResult = "9999"
	if actualResult != expectedResult {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

func getEnvVar(container corev1.Container, envName string) *corev1.EnvVar {
	for _, env := range container.Env {
		if env.Name == envName {
			return &env
		}
	}
	return nil
}

func checkContainerEnv(t *testing.T, container corev1.Container, envName string, expectedValue string) {
	env := getEnvVar(container, envName)
	if env != nil {
		if env.Value != "" {
			if env.Value != expectedValue {
				t.Fatalf("Expected `%v` but got `%v`", expectedValue, env.Value)
			}
		} else {
			// env.ValueFrom is the source for the environment variable's value. Cannot be used if value is not empty.
			// See https://pkg.go.dev/k8s.io/api/core/v1#EnvVar for more details.
			if env.ValueFrom.FieldRef.FieldPath != expectedValue {
				t.Fatalf("Expected `%v` but got `%v`", expectedValue, env.ValueFrom.FieldRef.FieldPath)
			}
		}
	} else {
		t.Fatalf("Couldn't find `%v` env on pod.", envName)
	}
}

func assertWorkerGCSFaultToleranceConfig(t *testing.T, podTemplate *corev1.PodTemplateSpec, container corev1.Container) {
	assert.Empty(t, podTemplate.Annotations[utils.RayExternalStorageNSAnnotationKey])
	assert.Empty(t, podTemplate.Annotations[utils.RayFTEnabledAnnotationKey])
	assert.True(t, utils.EnvVarExists(utils.RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, container.Env))
	assert.False(t, utils.EnvVarExists(utils.RAY_EXTERNAL_STORAGE_NS, container.Env))
	assert.False(t, utils.EnvVarExists(utils.RAY_REDIS_ADDRESS, container.Env))
	assert.False(t, utils.EnvVarExists(utils.REDIS_PASSWORD, container.Env))
}

func TestConfigureGCSFaultToleranceWithAnnotations(t *testing.T) {
	tests := []struct {
		name                        string
		storageNS                   string
		redisUsernameEnv            string
		redisPasswordEnv            string
		redisUsernameRayStartParams string
		redisPasswordRayStartParams string
		isHeadPod                   bool
		gcsFTEnabled                bool
	}{
		{
			name:         "GCS FT enabled",
			gcsFTEnabled: true,
			isHeadPod:    true,
		},
		{
			name:         "GCS FT enabled with external storage",
			gcsFTEnabled: true,
			storageNS:    "test-ns",
			isHeadPod:    true,
		},
		{
			name:             "GCS FT enabled with redis password env",
			gcsFTEnabled:     true,
			redisPasswordEnv: "test-password",
			isHeadPod:        true,
		},
		{
			name:             "GCS FT enabled with redis username and password env",
			gcsFTEnabled:     true,
			redisUsernameEnv: "test-username",
			redisPasswordEnv: "test-password",
			isHeadPod:        true,
		},
		{
			name:                        "GCS FT enabled with redis password ray start params",
			gcsFTEnabled:                true,
			redisPasswordRayStartParams: "test-password",
			isHeadPod:                   true,
		},
		{
			name:                        "GCS FT enabled with redis username and password ray start params",
			gcsFTEnabled:                true,
			redisUsernameRayStartParams: "test-username",
			redisPasswordRayStartParams: "test-password",
			isHeadPod:                   true,
		},
		{
			// The most common case.
			name:                        "GCS FT enabled with redis password env and ray start params referring to env",
			gcsFTEnabled:                true,
			redisPasswordEnv:            "test-password",
			redisPasswordRayStartParams: "$REDIS_PASSWORD",
			isHeadPod:                   false,
		},
		{
			name:                        "GCS FT enabled with redis username and password env and ray start params referring to env",
			gcsFTEnabled:                true,
			redisUsernameEnv:            "test-username",
			redisUsernameRayStartParams: "$REDIS_USERNAME",
			redisPasswordEnv:            "test-password",
			redisPasswordRayStartParams: "$REDIS_PASSWORD",
			isHeadPod:                   false,
		},
		{
			name:         "GCS FT enabled / worker Pod",
			gcsFTEnabled: true,
			isHeadPod:    false,
		},
		{
			name:         "GCS FT disabled",
			gcsFTEnabled: false,
			isHeadPod:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Validate the test input
			if test.redisUsernameEnv != "" && test.redisUsernameRayStartParams != "" {
				assert.True(t, test.redisUsernameRayStartParams == "$REDIS_USERNAME")
			}
			if test.redisPasswordEnv != "" && test.redisPasswordRayStartParams != "" {
				assert.True(t, test.redisPasswordRayStartParams == "$REDIS_PASSWORD")
			}

			// Prepare the cluster
			cluster := rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						utils.RayFTEnabledAnnotationKey: strconv.FormatBool(test.gcsFTEnabled),
					},
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						RayStartParams: map[string]string{},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Env: []corev1.EnvVar{
											{
												Name:  utils.RAY_REDIS_ADDRESS,
												Value: "redis:6379",
											},
										},
									},
								},
							},
						},
					},
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
						{
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Env: []corev1.EnvVar{},
										},
									},
								},
							},
						},
					},
				},
			}
			if test.storageNS != "" {
				cluster.Annotations[utils.RayExternalStorageNSAnnotationKey] = test.storageNS
			}
			if test.redisUsernameEnv != "" {
				cluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Env = append(cluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Env, corev1.EnvVar{
					Name:  utils.REDIS_USERNAME,
					Value: test.redisUsernameEnv,
				})
			}
			if test.redisPasswordEnv != "" {
				cluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Env = append(cluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Env, corev1.EnvVar{
					Name:  utils.REDIS_PASSWORD,
					Value: test.redisPasswordEnv,
				})
			}
			if test.redisUsernameRayStartParams != "" {
				cluster.Spec.HeadGroupSpec.RayStartParams["redis-username"] = test.redisUsernameRayStartParams
			}
			if test.redisPasswordRayStartParams != "" {
				cluster.Spec.HeadGroupSpec.RayStartParams["redis-password"] = test.redisPasswordRayStartParams
			}
			podTemplate := &cluster.Spec.HeadGroupSpec.Template
			if !test.isHeadPod {
				podTemplate = &cluster.Spec.WorkerGroupSpecs[0].Template
			}

			// Configure GCS fault tolerance
			if test.isHeadPod {
				configureGCSFaultTolerance(podTemplate, cluster, rayv1.HeadNode)
			} else {
				configureGCSFaultTolerance(podTemplate, cluster, rayv1.WorkerNode)
			}

			// Check configurations for GCS fault tolerance
			container := podTemplate.Spec.Containers[utils.RayContainerIndex]
			if test.isHeadPod {
				assert.False(t, utils.EnvVarExists(utils.RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, container.Env))
				assert.Equal(t, podTemplate.Annotations[utils.RayFTEnabledAnnotationKey], strconv.FormatBool(test.gcsFTEnabled))
				if test.storageNS != "" {
					assert.Equal(t, podTemplate.Annotations[utils.RayExternalStorageNSAnnotationKey], test.storageNS)
					assert.True(t, utils.EnvVarExists(utils.RAY_EXTERNAL_STORAGE_NS, container.Env))
				}
				if test.redisUsernameEnv != "" {
					env := getEnvVar(container, utils.REDIS_USERNAME)
					assert.Equal(t, env.Value, test.redisUsernameEnv)
				}
				if test.redisPasswordEnv != "" {
					env := getEnvVar(container, utils.REDIS_PASSWORD)
					assert.Equal(t, env.Value, test.redisPasswordEnv)
				} else if test.redisPasswordRayStartParams != "" {
					env := getEnvVar(container, utils.REDIS_PASSWORD)
					assert.Equal(t, env.Value, test.redisPasswordRayStartParams)
				}
			} else {
				assertWorkerGCSFaultToleranceConfig(t, podTemplate, container)
			}
		})
	}
}

func TestConfigureGCSFaultToleranceWithGcsFTOptions(t *testing.T) {
	tests := []struct {
		gcsFTOptions *rayv1.GcsFaultToleranceOptions
		name         string
		isHeadPod    bool
	}{
		{
			name: "GCS FT enabled",
			gcsFTOptions: &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
			},
			isHeadPod: true,
		},
		{
			name: "GCS FT enabled with redis password",
			gcsFTOptions: &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				RedisPassword: &rayv1.RedisCredential{
					Value: "test-password",
				},
			},
			isHeadPod: true,
		},
		{
			name: "GCS FT enabled with redis username and password",
			gcsFTOptions: &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				RedisUsername: &rayv1.RedisCredential{
					Value: "test-username",
				},
				RedisPassword: &rayv1.RedisCredential{
					Value: "test-password",
				},
			},
			isHeadPod: true,
		},
		{
			name: "GCS FT enabled with redis password in secret",
			gcsFTOptions: &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				RedisPassword: &rayv1.RedisCredential{
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.redisPassword",
						},
					},
				},
			},
			isHeadPod: true,
		},
		{
			name: "GCS FT enabled with redis username and password in secret",
			gcsFTOptions: &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
				RedisUsername: &rayv1.RedisCredential{
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.redisUsername",
						},
					},
				},
				RedisPassword: &rayv1.RedisCredential{
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.redisPassword",
						},
					},
				},
			},
			isHeadPod: true,
		},
		{
			name: "GCS FT enabled with redis external storage namespace",
			gcsFTOptions: &rayv1.GcsFaultToleranceOptions{
				RedisAddress:             "redis:6379",
				ExternalStorageNamespace: "test-ns",
			},
			isHeadPod: true,
		},
		{
			name: "GCS FT enabled / worker Pod",
			gcsFTOptions: &rayv1.GcsFaultToleranceOptions{
				RedisAddress: "redis:6379",
			},
			isHeadPod: false,
		},
	}

	emptyPodTemplate := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Env: []corev1.EnvVar{},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Prepare the cluster
			cluster := rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					GcsFaultToleranceOptions: test.gcsFTOptions,
					HeadGroupSpec: rayv1.HeadGroupSpec{
						RayStartParams: map[string]string{},
						Template:       *emptyPodTemplate.DeepCopy(),
					},
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
						{
							Template: *emptyPodTemplate.DeepCopy(),
						},
					},
				},
			}

			podTemplate := &cluster.Spec.HeadGroupSpec.Template
			nodeType := rayv1.HeadNode
			if !test.isHeadPod {
				podTemplate = &cluster.Spec.WorkerGroupSpecs[0].Template
				nodeType = rayv1.WorkerNode
			}
			configureGCSFaultTolerance(podTemplate, cluster, nodeType)
			container := podTemplate.Spec.Containers[utils.RayContainerIndex]

			if test.isHeadPod {
				assert.False(t, utils.EnvVarExists(utils.RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, container.Env))
				assert.Equal(t, podTemplate.Annotations[utils.RayFTEnabledAnnotationKey], strconv.FormatBool(test.gcsFTOptions != nil))

				env := getEnvVar(container, utils.RAY_REDIS_ADDRESS)
				assert.Equal(t, "redis:6379", env.Value)

				if test.gcsFTOptions.RedisUsername != nil {
					env := getEnvVar(container, utils.REDIS_USERNAME)
					assert.Equal(t, env.Value, test.gcsFTOptions.RedisUsername.Value)
					assert.Equal(t, env.ValueFrom, test.gcsFTOptions.RedisUsername.ValueFrom)
				}

				if test.gcsFTOptions.RedisPassword != nil {
					env := getEnvVar(container, utils.REDIS_PASSWORD)
					assert.Equal(t, env.Value, test.gcsFTOptions.RedisPassword.Value)
					assert.Equal(t, env.ValueFrom, test.gcsFTOptions.RedisPassword.ValueFrom)
				}
				if test.gcsFTOptions.ExternalStorageNamespace != "" {
					assert.Equal(t, podTemplate.Annotations[utils.RayExternalStorageNSAnnotationKey], test.gcsFTOptions.ExternalStorageNamespace)
					env := getEnvVar(container, utils.RAY_EXTERNAL_STORAGE_NS)
					assert.Equal(t, env.Value, test.gcsFTOptions.ExternalStorageNamespace)
				}
			} else {
				assertWorkerGCSFaultToleranceConfig(t, podTemplate, container)
			}
		})
	}
}

func TestBuildPod(t *testing.T) {
	cluster := instance.DeepCopy()
	ctx := context.Background()

	// Test head pod
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(ctx, podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", false, utils.GetCRDType(""), "")

	// Check environment variables
	rayContainer := pod.Spec.Containers[utils.RayContainerIndex]
	checkContainerEnv(t, rayContainer, utils.RAY_ADDRESS, "127.0.0.1:6379")
	checkContainerEnv(t, rayContainer, utils.RAY_USAGE_STATS_KUBERAY_IN_USE, "1")
	checkContainerEnv(t, rayContainer, utils.RAY_CLUSTER_NAME, fmt.Sprintf("metadata.labels['%s']", utils.RayClusterLabelKey))
	checkContainerEnv(t, rayContainer, utils.RAY_DASHBOARD_ENABLE_K8S_DISK_USAGE, "1")
	checkContainerEnv(t, rayContainer, utils.RAY_NODE_TYPE_NAME, fmt.Sprintf("metadata.labels['%s']", utils.RayNodeGroupLabelKey))
	checkContainerEnv(t, rayContainer, utils.RAY_USAGE_STATS_EXTRA_TAGS, fmt.Sprintf("kuberay_version=%s;kuberay_crd=%s", utils.KUBERAY_VERSION, utils.RayClusterCRD))
	headRayStartCommandEnv := getEnvVar(rayContainer, utils.KUBERAY_GEN_RAY_START_CMD)
	assert.True(t, strings.Contains(headRayStartCommandEnv.Value, "ray start"))

	// In head, init container needs FQ_RAY_IP to create a self-signed certificate for its TLS authenticate.
	for _, initContainer := range pod.Spec.InitContainers {
		checkContainerEnv(t, initContainer, utils.FQ_RAY_IP, "raycluster-sample-head-svc.default.svc.cluster.local")
		checkContainerEnv(t, rayContainer, utils.RAY_IP, "raycluster-sample-head-svc")
	}

	// Check labels.
	actualResult := pod.Labels[utils.RayClusterLabelKey]
	expectedResult := cluster.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[utils.RayNodeTypeLabelKey]
	expectedResult = string(rayv1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[utils.RayNodeGroupLabelKey]
	expectedResult = utils.RayNodeHeadGroupLabelValue
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	// Check volumes.
	actualVolumes := pod.Spec.Volumes
	expectedVolumes := volumesNoAutoscaler
	if !reflect.DeepEqual(actualVolumes, expectedVolumes) {
		t.Fatalf("Expected `%v` but got `%v`", expectedVolumes, actualVolumes)
	}

	// Check volume mounts.
	actualVolumeMounts := pod.Spec.Containers[0].VolumeMounts
	expectedVolumeMounts := volumeMountsNoAutoscaler
	if !reflect.DeepEqual(actualVolumeMounts, expectedVolumeMounts) {
		t.Fatalf("Expected `%v` but got `%v`", expectedVolumeMounts, actualVolumeMounts)
	}

	// testing worker pod
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, *cluster, cluster.Namespace)
	podTemplateSpec = DefaultWorkerPodTemplate(ctx, *cluster, worker, podName, fqdnRayIP, "6379")
	pod = BuildPod(ctx, podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, "6379", false, utils.GetCRDType(""), fqdnRayIP)

	// Check resources
	rayContainer = pod.Spec.Containers[utils.RayContainerIndex]
	expectedResources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: testMemoryLimit,
			"nvidia.com/gpu":      resource.MustParse("3"),
		},
	}
	assert.Equal(t, expectedResources.Limits, rayContainer.Resources.Limits, "Resource limits do not match")

	// Check environment variables
	rayContainer = pod.Spec.Containers[utils.RayContainerIndex]
	checkContainerEnv(t, rayContainer, utils.RAY_ADDRESS, "raycluster-sample-head-svc.default.svc.cluster.local:6379")
	checkContainerEnv(t, rayContainer, utils.FQ_RAY_IP, "raycluster-sample-head-svc.default.svc.cluster.local")
	checkContainerEnv(t, rayContainer, utils.RAY_IP, "raycluster-sample-head-svc")
	checkContainerEnv(t, rayContainer, utils.RAY_CLUSTER_NAME, fmt.Sprintf("metadata.labels['%s']", utils.RayClusterLabelKey))
	checkContainerEnv(t, rayContainer, utils.RAY_DASHBOARD_ENABLE_K8S_DISK_USAGE, "1")
	checkContainerEnv(t, rayContainer, utils.RAY_NODE_TYPE_NAME, fmt.Sprintf("metadata.labels['%s']", utils.RayNodeGroupLabelKey))
	workerRayStartCommandEnv := getEnvVar(rayContainer, utils.KUBERAY_GEN_RAY_START_CMD)
	assert.True(t, strings.Contains(workerRayStartCommandEnv.Value, "ray start"))

	expectedCommandArg := splitAndSort("ulimit -n 65536; ray start --block --dashboard-agent-listen-port=52365 --memory=1073741824 --num-cpus=1 --num-gpus=3 --address=raycluster-sample-head-svc.default.svc.cluster.local:6379 --port=6379 --metrics-export-port=8080")
	actualCommandArg := splitAndSort(pod.Spec.Containers[0].Args[0])
	if !reflect.DeepEqual(expectedCommandArg, actualCommandArg) {
		t.Fatalf("Expected `%v` but got `%v`", expectedCommandArg, actualCommandArg)
	}

	// Check Envs
	rayContainer = pod.Spec.Containers[utils.RayContainerIndex]
	checkContainerEnv(t, rayContainer, "TEST_ENV_NAME", "TEST_ENV_VALUE")
}

func TestBuildPod_WithNoCPULimits(t *testing.T) {
	cluster := instance.DeepCopy()
	ctx := context.Background()

	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: testMemoryLimit,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: testMemoryLimit,
		},
	}
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[utils.RayContainerIndex].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: testMemoryLimit,
		},

		Limits: corev1.ResourceList{
			corev1.ResourceMemory: testMemoryLimit,
			"nvidia.com/gpu":      resource.MustParse("3"),
		},
	}

	// Test head pod
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(ctx, podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", false, utils.GetCRDType(""), "")
	expectedCommandArg := splitAndSort("ulimit -n 65536; ray start --head --block --dashboard-agent-listen-port=52365 --memory=1073741824 --num-cpus=2 --metrics-export-port=8080 --dashboard-host=0.0.0.0")
	actualCommandArg := splitAndSort(pod.Spec.Containers[0].Args[0])
	if !reflect.DeepEqual(expectedCommandArg, actualCommandArg) {
		t.Fatalf("Expected `%v` but got `%v`", expectedCommandArg, actualCommandArg)
	}

	// testing worker pod
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, *cluster, cluster.Namespace)
	podTemplateSpec = DefaultWorkerPodTemplate(ctx, *cluster, worker, podName, fqdnRayIP, "6379")
	pod = BuildPod(ctx, podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, "6379", false, utils.GetCRDType(""), fqdnRayIP)
	expectedCommandArg = splitAndSort("ulimit -n 65536; ray start --block --dashboard-agent-listen-port=52365 --memory=1073741824 --num-cpus=2 --num-gpus=3 --address=raycluster-sample-head-svc.default.svc.cluster.local:6379 --port=6379 --metrics-export-port=8080")
	actualCommandArg = splitAndSort(pod.Spec.Containers[0].Args[0])
	if !reflect.DeepEqual(expectedCommandArg, actualCommandArg) {
		t.Fatalf("Expected `%v` but got `%v`", expectedCommandArg, actualCommandArg)
	}
}

func TestBuildPod_WithOverwriteCommand(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	cluster.Annotations = map[string]string{
		// When the value of the annotation is "true", KubeRay will not generate the command and args for the container.
		// Instead, it will use the command and args specified by the use.
		utils.RayOverwriteContainerCmdAnnotationKey: "true",
	}
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Command = []string{"I am head"}
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Args = []string{"I am head again"}
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[utils.RayContainerIndex].Command = []string{"I am worker"}
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[utils.RayContainerIndex].Args = []string{"I am worker again"}

	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	headPod := BuildPod(ctx, podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", false, utils.GetCRDType(""), "")
	headContainer := headPod.Spec.Containers[utils.RayContainerIndex]
	assert.Equal(t, []string{"I am head"}, headContainer.Command)
	assert.Equal(t, []string{"I am head again"}, headContainer.Args)

	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, *cluster, cluster.Namespace)
	podTemplateSpec = DefaultWorkerPodTemplate(ctx, *cluster, worker, podName, fqdnRayIP, "6379")
	workerPod := BuildPod(ctx, podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, "6379", false, utils.GetCRDType(""), fqdnRayIP)
	workerContainer := workerPod.Spec.Containers[utils.RayContainerIndex]
	assert.Equal(t, []string{"I am worker"}, workerContainer.Command)
	assert.Equal(t, []string{"I am worker again"}, workerContainer.Args)
}

func TestBuildPod_WithAutoscalerEnabled(t *testing.T) {
	ctx := context.Background()
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(ctx, podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", true, utils.GetCRDType(""), "")

	actualResult := pod.Labels[utils.RayClusterLabelKey]
	expectedResult := cluster.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[utils.RayNodeTypeLabelKey]
	expectedResult = string(rayv1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[utils.RayNodeGroupLabelKey]
	expectedResult = utils.RayNodeHeadGroupLabelValue
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	// verify no-monitoring is set. conversion only happens in BuildPod so we can only check it here.
	expectedResult = "--no-monitor"
	if !strings.Contains(pod.Spec.Containers[0].Args[0], expectedResult) {
		t.Fatalf("Expected `%v` in `%v` but doesn't have the config", expectedResult, pod.Spec.Containers[0].Args[0])
	}

	actualVolumes := pod.Spec.Volumes
	expectedVolumes := volumesWithAutoscaler
	if !reflect.DeepEqual(actualVolumes, expectedVolumes) {
		t.Fatalf("Expected `%v` but got `%v`", actualVolumes, expectedVolumes)
	}

	actualVolumeMounts := pod.Spec.Containers[0].VolumeMounts
	expectedVolumeMounts := volumeMountsWithAutoscaler
	if !reflect.DeepEqual(actualVolumeMounts, expectedVolumeMounts) {
		t.Fatalf("Expected `%v` but got `%v`", expectedVolumeMounts, actualVolumeMounts)
	}

	// Make sure autoscaler container was formatted correctly.
	numContainers := len(pod.Spec.Containers)
	expectedNumContainers := 2
	if !(numContainers == expectedNumContainers) {
		t.Fatalf("Expected `%v` container but got `%v`", expectedNumContainers, numContainers)
	}
	index := getAutoscalerContainerIndex(pod)
	actualContainer := pod.Spec.Containers[index]
	expectedContainer := autoscalerContainer
	if !reflect.DeepEqual(expectedContainer, actualContainer) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainer, actualContainer)
	}
}

func TestBuildPod_WithCreatedByRayService(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(ctx, podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", true, utils.RayServiceCRD, "")

	val, ok := pod.Labels[utils.RayClusterServingServiceLabelKey]
	assert.True(t, ok, "Expected serve label is not present")
	assert.Equal(t, utils.EnableRayClusterServingServiceFalse, val, "Wrong serve label value")
	utils.EnvVarExists(utils.RAY_TIMEOUT_MS_TASK_WAIT_FOR_DEATH_INFO, pod.Spec.Containers[utils.RayContainerIndex].Env)

	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, *cluster, cluster.Namespace)
	podTemplateSpec = DefaultWorkerPodTemplate(ctx, *cluster, worker, podName, fqdnRayIP, "6379")
	pod = BuildPod(ctx, podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, "6379", false, utils.RayServiceCRD, fqdnRayIP)

	val, ok = pod.Labels[utils.RayClusterServingServiceLabelKey]
	assert.True(t, ok, "Expected serve label is not present")
	assert.Equal(t, utils.EnableRayClusterServingServiceTrue, val, "Wrong serve label value")
	utils.EnvVarExists(utils.RAY_TIMEOUT_MS_TASK_WAIT_FOR_DEATH_INFO, pod.Spec.Containers[utils.RayContainerIndex].Env)
}

// Check that autoscaler container overrides work as expected.
func TestBuildPodWithAutoscalerOptions(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))

	customAutoscalerImage := "custom-autoscaler-xxx"
	customPullPolicy := corev1.PullIfNotPresent
	customTimeout := int32(100)
	customUpscaling := rayv1.UpscalingMode("Aggressive")
	customResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: testMemoryLimit,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: testMemoryLimit,
		},
	}
	customEnv := []corev1.EnvVar{{Name: "fooEnv", Value: "fooValue"}}
	customEnvFrom := []corev1.EnvFromSource{{Prefix: "Pre"}}
	customVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "ca-tls",
			MountPath: "/etc/ca/tls",
			ReadOnly:  true,
		},
		{
			Name:      "ray-tls",
			MountPath: "/etc/ray/tls",
		},
	}

	// Define a custom security profile.
	allowPrivilegeEscalation := false
	capabilities := corev1.Capabilities{
		Drop: []corev1.Capability{"ALL"},
	}
	seccompProfile := corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}
	runAsNonRoot := true
	customSecurityContext := corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		Capabilities:             &capabilities,
		RunAsNonRoot:             &runAsNonRoot,
		SeccompProfile:           &seccompProfile,
	}

	cluster.Spec.AutoscalerOptions = &rayv1.AutoscalerOptions{
		UpscalingMode:      &customUpscaling,
		IdleTimeoutSeconds: &customTimeout,
		Image:              &customAutoscalerImage,
		ImagePullPolicy:    &customPullPolicy,
		Resources:          &customResources,
		Env:                customEnv,
		EnvFrom:            customEnvFrom,
		VolumeMounts:       customVolumeMounts,
		SecurityContext:    &customSecurityContext,
	}
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(ctx, podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", true, utils.GetCRDType(""), "")
	expectedContainer := *autoscalerContainer.DeepCopy()
	expectedContainer.Image = customAutoscalerImage
	expectedContainer.ImagePullPolicy = customPullPolicy
	expectedContainer.Resources = customResources
	expectedContainer.EnvFrom = customEnvFrom
	expectedContainer.Env = append(expectedContainer.Env, customEnv...)
	expectedContainer.VolumeMounts = append(customVolumeMounts, expectedContainer.VolumeMounts...)
	expectedContainer.SecurityContext = &customSecurityContext
	index := getAutoscalerContainerIndex(pod)
	actualContainer := pod.Spec.Containers[index]
	if !reflect.DeepEqual(expectedContainer, actualContainer) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainer, actualContainer)
	}
}

func TestHeadPodTemplate_WithAutoscalingEnabled(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")

	// autoscaler container is injected into head pod
	actualContainerCount := len(podTemplateSpec.Spec.Containers)
	expectedContainerCount := 2
	if !reflect.DeepEqual(expectedContainerCount, actualContainerCount) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainerCount, actualContainerCount)
	}

	actualResult := podTemplateSpec.Spec.ServiceAccountName
	expectedResult := cluster.Name

	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	// Repeat ServiceAccountName check with long cluster name.
	cluster.Name = longString(t) // 200 chars long
	podTemplateSpec = DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	actualResult = podTemplateSpec.Spec.ServiceAccountName
	expectedResult = shortString(t) // 50 chars long, truncated by utils.CheckName
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

func TestHeadPodTemplate_AutoscalerImage(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	cluster.Spec.AutoscalerOptions = nil
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))

	// Case 1: If `AutoscalerOptions.Image` is not set, the Autoscaler container should use the Ray head container's image by default.
	expectedAutoscalerImage := cluster.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex].Image
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := corev1.Pod{
		Spec: podTemplateSpec.Spec,
	}
	autoscalerContainerIndex := getAutoscalerContainerIndex(pod)
	assert.Equal(t, expectedAutoscalerImage, podTemplateSpec.Spec.Containers[autoscalerContainerIndex].Image)

	// Case 2: If `AutoscalerOptions.Image` is set, the Autoscaler container should use the specified image.
	customAutoscalerImage := "custom-autoscaler-xxx"
	cluster = instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	cluster.Spec.AutoscalerOptions = &rayv1.AutoscalerOptions{
		Image: &customAutoscalerImage,
	}
	podTemplateSpec = DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod.Spec = podTemplateSpec.Spec
	autoscalerContainerIndex = getAutoscalerContainerIndex(pod)
	assert.Equal(t, customAutoscalerImage, podTemplateSpec.Spec.Containers[autoscalerContainerIndex].Image)
}

// If no service account is specified in the RayCluster,
// the head pod's service account should be an empty string.
func TestHeadPodTemplate_WithNoServiceAccount(t *testing.T) {
	cluster := instance.DeepCopy()
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	pod := DefaultHeadPodTemplate(context.Background(), *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")

	actualResult := pod.Spec.ServiceAccountName
	expectedResult := ""
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

// If a service account is specified in the RayCluster and EnableInTreeAutoscaling is set to false,
// the head pod's service account should be the same.
func TestHeadPodTemplate_WithServiceAccountNoAutoscaling(t *testing.T) {
	cluster := instance.DeepCopy()
	serviceAccount := "head-service-account"
	cluster.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName = serviceAccount
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	pod := DefaultHeadPodTemplate(context.Background(), *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")

	actualResult := pod.Spec.ServiceAccountName
	expectedResult := serviceAccount
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

// If a service account is specified in the RayCluster and EnableInTreeAutoscaling is set to true,
// the head pod's service account should be the same.
func TestHeadPodTemplate_WithServiceAccount(t *testing.T) {
	cluster := instance.DeepCopy()
	serviceAccount := "head-service-account"
	cluster.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName = serviceAccount
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	pod := DefaultHeadPodTemplate(context.Background(), *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")

	actualResult := pod.Spec.ServiceAccountName
	expectedResult := serviceAccount
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

func splitAndSort(s string) []string {
	strs := strings.Split(s, " ")
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if len(s) > 0 {
			result = append(result, s)
		}
	}
	sort.Strings(result)
	return result
}

func TestDefaultWorkerPodTemplateWithName(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, *cluster, cluster.Namespace)
	worker := cluster.Spec.WorkerGroupSpecs[0]
	worker.Template.ObjectMeta.Name = "ray-worker-test"
	podName := cluster.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol + utils.FormatInt32(0)
	expectedWorker := *worker.DeepCopy()

	// Pass a deep copy of worker (*worker.DeepCopy()) to prevent "worker" from updating.
	podTemplateSpec := DefaultWorkerPodTemplate(ctx, *cluster, *worker.DeepCopy(), podName, fqdnRayIP, "6379")
	assert.Empty(t, podTemplateSpec.ObjectMeta.Name)
	assert.Equal(t, expectedWorker, worker)
}

func containerPortExists(ports []corev1.ContainerPort, containerPort int32) error {
	name := utils.MetricsPortName
	for _, port := range ports {
		if port.Name == name {
			if port.ContainerPort != containerPort {
				return fmt.Errorf("expected `%v` but got `%v` for `%v` port", containerPort, port.ContainerPort, name)
			}
			return nil
		}
	}
	return fmt.Errorf("couldn't find `%v` port", name)
}

func TestDefaultHeadPodTemplateWithConfigurablePorts(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{}
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	// DefaultHeadPodTemplate will add the default metrics port if user doesn't specify it.
	// Verify the default metrics port exists.
	if err := containerPortExists(podTemplateSpec.Spec.Containers[0].Ports, int32(utils.DefaultMetricsPort)); err != nil {
		t.Fatal(err)
	}
	customMetricsPort := int32(utils.DefaultMetricsPort) + 1
	metricsPort := corev1.ContainerPort{
		Name:          utils.MetricsPortName,
		ContainerPort: customMetricsPort,
	}
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{metricsPort}
	podTemplateSpec = DefaultHeadPodTemplate(ctx, *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	// Verify the custom metrics port exists.
	if err := containerPortExists(podTemplateSpec.Spec.Containers[0].Ports, customMetricsPort); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultWorkerPodTemplateWithConfigurablePorts(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Ports = []corev1.ContainerPort{}
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName := cluster.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, *cluster, cluster.Namespace)
	podTemplateSpec := DefaultWorkerPodTemplate(ctx, *cluster, worker, podName, fqdnRayIP, "6379")
	// DefaultWorkerPodTemplate will add the default metrics port if user doesn't specify it.
	// Verify the default metrics port exists.
	if err := containerPortExists(podTemplateSpec.Spec.Containers[0].Ports, int32(utils.DefaultMetricsPort)); err != nil {
		t.Fatal(err)
	}
	customMetricsPort := int32(utils.DefaultMetricsPort) + 1
	metricsPort := corev1.ContainerPort{
		Name:          utils.MetricsPortName,
		ContainerPort: customMetricsPort,
	}
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Ports = []corev1.ContainerPort{metricsPort}
	podTemplateSpec = DefaultWorkerPodTemplate(ctx, *cluster, worker, podName, fqdnRayIP, "6379")
	// Verify the custom metrics port exists.
	if err := containerPortExists(podTemplateSpec.Spec.Containers[0].Ports, customMetricsPort); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultInitContainer(t *testing.T) {
	ctx := context.Background()
	// A default init container to check the health of GCS is expected to be added.
	cluster := instance.DeepCopy()
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, *cluster, cluster.Namespace)
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName := cluster.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol + utils.FormatInt32(0)
	expectedResult := len(cluster.Spec.WorkerGroupSpecs[0].Template.Spec.InitContainers) + 1

	// Pass a deep copy of worker (*worker.DeepCopy()) to prevent "worker" from updating.
	podTemplateSpec := DefaultWorkerPodTemplate(ctx, *cluster, *worker.DeepCopy(), podName, fqdnRayIP, "6379")
	numInitContainers := len(podTemplateSpec.Spec.InitContainers)
	assert.Equal(t, expectedResult, numInitContainers, "A default init container is expected to be added.")

	// This health-check init container requires certain environment variables to establish a secure connection
	// with the Ray head using TLS authentication. Currently, we simply copied all environment variables from
	// Ray container to the init container. This may be changed in the future.
	healthCheckContainer := podTemplateSpec.Spec.InitContainers[numInitContainers-1]
	rayContainer := worker.Template.Spec.Containers[utils.RayContainerIndex]

	assert.NotEmpty(t, rayContainer.Env, "The test only makes sense if the Ray container has environment variables.")
	assert.Equal(t, len(rayContainer.Env), len(healthCheckContainer.Env))
	for _, env := range rayContainer.Env {
		// env.ValueFrom is the source for the environment variable's value. Cannot be used if value is not empty.
		if env.Value != "" {
			checkContainerEnv(t, healthCheckContainer, env.Name, env.Value)
		} else {
			checkContainerEnv(t, healthCheckContainer, env.Name, env.ValueFrom.FieldRef.FieldPath)
		}
	}

	assert.NotEmpty(t, rayContainer.Resources, "The test only makes sense if the Ray container has resource limit/request.")
}

func TestDefaultInitContainerImagePullPolicy(t *testing.T) {
	ctx := context.Background()

	cluster := instance.DeepCopy()
	fqdnRayIP := utils.GenerateFQDNServiceName(ctx, *cluster, cluster.Namespace)
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName := cluster.Name + utils.DashSymbol + string(rayv1.WorkerNode) + utils.DashSymbol + worker.GroupName + utils.DashSymbol + utils.FormatInt32(0)

	cases := []struct {
		name               string
		imagePullPolicy    corev1.PullPolicy
		expectedPullPolicy corev1.PullPolicy
	}{
		{
			name:               "Always",
			imagePullPolicy:    corev1.PullAlways,
			expectedPullPolicy: corev1.PullAlways,
		},
		{
			name:               "IfNotPresent",
			imagePullPolicy:    corev1.PullIfNotPresent,
			expectedPullPolicy: corev1.PullIfNotPresent,
		},
		{
			name:               "Never",
			imagePullPolicy:    corev1.PullNever,
			expectedPullPolicy: corev1.PullNever,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// set ray container imagePullPolicy
			worker.Template.Spec.Containers[utils.RayContainerIndex].ImagePullPolicy = tc.imagePullPolicy

			podTemplateSpec := DefaultWorkerPodTemplate(ctx, *cluster, *worker.DeepCopy(), podName, fqdnRayIP, "6379")

			healthCheckContainer := podTemplateSpec.Spec.InitContainers[len(podTemplateSpec.Spec.InitContainers)-1]
			assert.Equal(t, tc.expectedPullPolicy, healthCheckContainer.ImagePullPolicy, "The ImagePullPolicy of the init container should be the same as the Ray container.")
		})
	}
}

func TestSetMissingRayStartParamsAddress(t *testing.T) {
	ctx := context.Background()
	// The address option is automatically injected into RayStartParams with a default value of <Fully Qualified Domain Name (FQDN)>:<headPort> for workers only, which is used to connect to the Ray cluster.
	// The head should not include the address option in RayStartParams, as it can access the GCS server, which also runs on the head, via localhost.
	// Although not recommended, users can manually set the address option for both head and workers.

	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"
	customAddress := "custom-address:1234"

	testCases := []struct {
		name           string
		assertion      func(t *testing.T, rayStartParams map[string]string)
		rayStartParams map[string]string
		fqdnRayIP      string
		nodeType       rayv1.RayNodeType
	}{
		{
			name:           "Head node with no address option set.",
			rayStartParams: map[string]string{},
			fqdnRayIP:      "",
			nodeType:       rayv1.HeadNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.NotContains(t, rayStartParams, "address", "Head node should not have an address option set by default.")
			},
		},
		{
			name:           "Head node with custom address option set.",
			rayStartParams: map[string]string{"address": customAddress},
			fqdnRayIP:      "",
			nodeType:       rayv1.HeadNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, customAddress, rayStartParams["address"], "Expected `%v` but got `%v`", customAddress, rayStartParams["address"])
			},
		},
		{
			name:           "Worker node with no address option set.",
			rayStartParams: map[string]string{},
			fqdnRayIP:      fqdnRayIP,
			nodeType:       rayv1.WorkerNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				expectedAddress := fmt.Sprintf("%s:%s", fqdnRayIP, headPort)
				assert.Equalf(t, expectedAddress, rayStartParams["address"], "Expected `%v` but got `%v`", expectedAddress, rayStartParams["address"])
			},
		},
		{
			name:           "Worker node with custom address option set.",
			rayStartParams: map[string]string{"address": customAddress},
			fqdnRayIP:      fqdnRayIP,
			nodeType:       rayv1.WorkerNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, customAddress, rayStartParams["address"], "Expected `%v` but got `%v`", customAddress, rayStartParams["address"])
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.rayStartParams = setMissingRayStartParams(ctx, testCase.rayStartParams, testCase.nodeType, headPort, testCase.fqdnRayIP)
			testCase.assertion(t, testCase.rayStartParams)
		})
	}
}

func TestSetMissingRayStartParamsMetricsExportPort(t *testing.T) {
	ctx := context.Background()

	// The metrics-export-port option is automatically injected into RayStartParams with a default value of DefaultMetricsPort for both head and workers.
	// Users can manually set the metrics-export-port option to customize the metrics export port and scrape Ray’s metrics using Prometheus via <ip>:<custom metrics export port>.
	// See https://github.com/ray-project/kuberay/pull/954 for more details.

	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"
	customMetricsPort := utils.DefaultMetricsPort + 1
	testCases := []struct {
		name           string
		assertion      func(t *testing.T, rayStartParams map[string]string)
		rayStartParams map[string]string
		fqdnRayIP      string
		nodeType       rayv1.RayNodeType
	}{
		{
			name:           "Head node with no metrics-export-port option set.",
			rayStartParams: map[string]string{},
			fqdnRayIP:      "",
			nodeType:       rayv1.HeadNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, fmt.Sprint(utils.DefaultMetricsPort), rayStartParams["metrics-export-port"], "Expected `%v` but got `%v`", utils.DefaultMetricsPort, rayStartParams["metrics-export-port"])
			},
		},
		{
			name:           "Head node with custom metrics-export-port option set.",
			rayStartParams: map[string]string{"metrics-export-port": fmt.Sprint(customMetricsPort)},
			fqdnRayIP:      "",
			nodeType:       rayv1.HeadNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, fmt.Sprint(customMetricsPort), rayStartParams["metrics-export-port"], "Expected `%v` but got `%v`", customMetricsPort, rayStartParams["metrics-export-port"])
			},
		},
		{
			name:           "Worker node with no metrics-export-port option set.",
			rayStartParams: map[string]string{},
			fqdnRayIP:      fqdnRayIP,
			nodeType:       rayv1.WorkerNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, fmt.Sprint(utils.DefaultMetricsPort), rayStartParams["metrics-export-port"], "Expected `%v` but got `%v`", utils.DefaultMetricsPort, rayStartParams["metrics-export-port"])
			},
		},
		{
			name:           "Worker node with custom metrics-export-port option set.",
			rayStartParams: map[string]string{"metrics-export-port": fmt.Sprint(customMetricsPort)},
			fqdnRayIP:      fqdnRayIP,
			nodeType:       rayv1.WorkerNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, fmt.Sprint(customMetricsPort), rayStartParams["metrics-export-port"], "Expected `%v` but got `%v`", customMetricsPort, rayStartParams["metrics-export-port"])
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.rayStartParams = setMissingRayStartParams(ctx, testCase.rayStartParams, testCase.nodeType, headPort, testCase.fqdnRayIP)
			testCase.assertion(t, testCase.rayStartParams)
		})
	}
}

func TestSetMissingRayStartParamsBlock(t *testing.T) {
	ctx := context.Background()

	// The --block option is automatically injected into RayStartParams with a default value of true for both head and workers.
	// Although not recommended, users can manually set the --block option.
	// See https://github.com/ray-project/kuberay/pull/675 for more details.

	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"
	testCases := []struct {
		name           string
		assertion      func(t *testing.T, rayStartParams map[string]string)
		rayStartParams map[string]string
		fqdnRayIP      string
		nodeType       rayv1.RayNodeType
	}{
		{
			name:           "Head node with no --block option set.",
			rayStartParams: map[string]string{},
			fqdnRayIP:      "",
			nodeType:       rayv1.HeadNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, "true", rayStartParams["block"], "Expected `%v` but got `%v`", "true", rayStartParams["block"])
			},
		},
		{
			name:           "Head node with --block option set to false.",
			rayStartParams: map[string]string{"block": "false"},
			fqdnRayIP:      "",
			nodeType:       rayv1.HeadNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, "true", rayStartParams["block"], "Expected `%v` but got `%v`", "false", rayStartParams["block"])
			},
		},
		{
			name:           "Worker node with no --block option set.",
			rayStartParams: map[string]string{},
			fqdnRayIP:      fqdnRayIP,
			nodeType:       rayv1.WorkerNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, "true", rayStartParams["block"], "Expected `%v` but got `%v`", "true", rayStartParams["block"])
			},
		},
		{
			name:           "Worker node with --block option set to false.",
			rayStartParams: map[string]string{"block": "false"},
			fqdnRayIP:      fqdnRayIP,
			nodeType:       rayv1.WorkerNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, "true", rayStartParams["block"], "Expected `%v` but got `%v`", "false", rayStartParams["block"])
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.rayStartParams = setMissingRayStartParams(ctx, testCase.rayStartParams, testCase.nodeType, headPort, testCase.fqdnRayIP)
			testCase.assertion(t, testCase.rayStartParams)
		})
	}
}

func TestSetMissingRayStartParamsDashboardHost(t *testing.T) {
	ctx := context.Background()

	// The dashboard-host option is automatically injected into RayStartParams with a default value of "0.0.0.0" for head only as workers do not have dashborad server.
	// Users can manually set the dashboard-host option to customize the host the dashboard server binds to, either "localhost" (127.0.0.1) or "0.0.0.0" (available from all interfaces).
	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"
	testCases := []struct {
		name           string
		assertion      func(t *testing.T, rayStartParams map[string]string)
		rayStartParams map[string]string
		fqdnRayIP      string
		nodeType       rayv1.RayNodeType
	}{
		{
			name:           "Head node with no dashboard-host option set.",
			rayStartParams: map[string]string{},
			fqdnRayIP:      "",
			nodeType:       rayv1.HeadNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, "0.0.0.0", rayStartParams["dashboard-host"], "Expected `%v` but got `%v`", "0.0.0.0", rayStartParams["dashboard-host"])
			},
		},
		{
			name:           "Head node with dashboard-host option set.",
			rayStartParams: map[string]string{"dashboard-host": "localhost"},
			fqdnRayIP:      "",
			nodeType:       rayv1.HeadNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, "localhost", rayStartParams["dashboard-host"], "Expected `%v` but got `%v`", "localhost", rayStartParams["dashboard-host"])
			},
		},
		{
			name:           "Worker node with no dashboard-host option set.",
			rayStartParams: map[string]string{},
			fqdnRayIP:      fqdnRayIP,
			nodeType:       rayv1.WorkerNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.NotContains(t, rayStartParams, "dashboard-host", "workers should not have an dashboard-host option set.")
			},
		},
		{
			name:           "Worker node with dashboard-host option set.",
			rayStartParams: map[string]string{"dashboard-host": "localhost"},
			fqdnRayIP:      fqdnRayIP,
			nodeType:       rayv1.WorkerNode,
			assertion: func(t *testing.T, rayStartParams map[string]string) {
				assert.Equalf(t, "localhost", rayStartParams["dashboard-host"], "Expected `%v` but got `%v`", "localhost", rayStartParams["dashboard-host"])
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.rayStartParams = setMissingRayStartParams(ctx, testCase.rayStartParams, testCase.nodeType, headPort, testCase.fqdnRayIP)
			testCase.assertion(t, testCase.rayStartParams)
		})
	}
}

func TestGetCustomWorkerInitImage(t *testing.T) {
	// cleanup
	defer os.Unsetenv(EnableInitContainerInjectionEnvKey)

	// not set the env
	b := getEnableInitContainerInjection()
	assert.True(t, b)
	// set the env with "true"
	os.Setenv(EnableInitContainerInjectionEnvKey, "true")
	b = getEnableInitContainerInjection()
	assert.True(t, b)
	// set the env with "True"
	os.Setenv(EnableInitContainerInjectionEnvKey, "True")
	b = getEnableInitContainerInjection()
	assert.True(t, b)
	// set the env with "false"
	os.Setenv(EnableInitContainerInjectionEnvKey, "false")
	b = getEnableInitContainerInjection()
	assert.False(t, b)
	// set the env with "False"
	os.Setenv(EnableInitContainerInjectionEnvKey, "False")
	b = getEnableInitContainerInjection()
	assert.False(t, b)
}

func TestGetEnableProbesInjection(t *testing.T) {
	// cleanup
	defer os.Unsetenv(utils.ENABLE_PROBES_INJECTION)

	// not set the env
	os.Unsetenv(utils.ENABLE_PROBES_INJECTION)
	b := getEnableProbesInjection()
	assert.True(t, b)
	// set the env with "true"
	os.Setenv(utils.ENABLE_PROBES_INJECTION, "true")
	b = getEnableProbesInjection()
	assert.True(t, b)
	// set the env with "True"
	os.Setenv(utils.ENABLE_PROBES_INJECTION, "True")
	b = getEnableProbesInjection()
	assert.True(t, b)
	// set the env with "false"
	os.Setenv(utils.ENABLE_PROBES_INJECTION, "false")
	b = getEnableProbesInjection()
	assert.False(t, b)
	// set the env with "False"
	os.Setenv(utils.ENABLE_PROBES_INJECTION, "False")
	b = getEnableProbesInjection()
	assert.False(t, b)
}

func TestInitLivenessAndReadinessProbe(t *testing.T) {
	cluster := instance.DeepCopy()
	podName := strings.ToLower(cluster.Name + utils.DashSymbol + string(rayv1.HeadNode) + utils.DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(context.Background(), *cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	rayContainer := &podTemplateSpec.Spec.Containers[utils.RayContainerIndex]

	// Test 1: User defines a custom HTTPGet probe.
	httpGetProbe := corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				// Check Raylet status
				Path: fmt.Sprintf("/%s", utils.RayAgentRayletHealthPath),
				Port: intstr.FromInt(utils.DefaultDashboardAgentListenPort),
			},
		},
	}

	rayContainer.LivenessProbe = &httpGetProbe
	rayContainer.ReadinessProbe = &httpGetProbe
	initLivenessAndReadinessProbe(rayContainer, rayv1.HeadNode, "")
	assert.NotNil(t, rayContainer.LivenessProbe.HTTPGet)
	assert.NotNil(t, rayContainer.ReadinessProbe.HTTPGet)
	assert.Nil(t, rayContainer.LivenessProbe.Exec)
	assert.Nil(t, rayContainer.ReadinessProbe.Exec)

	// Test 2: User does not define a custom probe. KubeRay will inject Exec probe for worker pod.
	// Here we test the case where the Ray Pod originates from RayServiceCRD,
	// implying that an additional serve health check will be added to the readiness probe.
	rayContainer.LivenessProbe = nil
	rayContainer.ReadinessProbe = nil
	initLivenessAndReadinessProbe(rayContainer, rayv1.WorkerNode, utils.RayServiceCRD)
	assert.NotNil(t, rayContainer.LivenessProbe.Exec)
	assert.NotNil(t, rayContainer.ReadinessProbe.Exec)
	assert.False(t, strings.Contains(strings.Join(rayContainer.LivenessProbe.Exec.Command, " "), utils.RayServeProxyHealthPath))
	assert.True(t, strings.Contains(strings.Join(rayContainer.ReadinessProbe.Exec.Command, " "), utils.RayServeProxyHealthPath))
	assert.Equal(t, int32(2), rayContainer.LivenessProbe.TimeoutSeconds)
	assert.Equal(t, int32(2), rayContainer.ReadinessProbe.TimeoutSeconds)

	// Test 3: User does not define a custom probe. KubeRay will inject Exec probe for head pod.
	// Here we test the case where the Ray Pod originates from RayServiceCRD,
	// implying that an additional serve health check will be added to the readiness probe.
	rayContainer.LivenessProbe = nil
	rayContainer.ReadinessProbe = nil
	initLivenessAndReadinessProbe(rayContainer, rayv1.HeadNode, utils.RayServiceCRD)
	assert.NotNil(t, rayContainer.LivenessProbe.Exec)
	assert.NotNil(t, rayContainer.ReadinessProbe.Exec)
	// head pod should not have Ray Serve proxy health probes
	assert.False(t, strings.Contains(strings.Join(rayContainer.LivenessProbe.Exec.Command, " "), utils.RayServeProxyHealthPath))
	assert.False(t, strings.Contains(strings.Join(rayContainer.ReadinessProbe.Exec.Command, " "), utils.RayServeProxyHealthPath))
	assert.Equal(t, int32(5), rayContainer.LivenessProbe.TimeoutSeconds)
	assert.Equal(t, int32(5), rayContainer.ReadinessProbe.TimeoutSeconds)
}

func TestGenerateRayStartCommand(t *testing.T) {
	tests := []struct {
		rayStartParams map[string]string
		name           string
		expected       string
		nodeType       rayv1.RayNodeType
		resource       corev1.ResourceRequirements
	}{
		{
			name:           "WorkerNode with GPU",
			nodeType:       rayv1.WorkerNode,
			rayStartParams: map[string]string{},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
			},
			expected: "ray start  --num-gpus=1 ",
		},
		{
			name:           "WorkerNode with TPU",
			nodeType:       rayv1.WorkerNode,
			rayStartParams: map[string]string{},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"google.com/tpu": resource.MustParse("4"),
				},
			},
			expected: `ray start  --resources='{"TPU":4}' `,
		},
		{
			name:           "HeadNode with Neuron Cores",
			nodeType:       rayv1.HeadNode,
			rayStartParams: map[string]string{},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"aws.amazon.com/neuroncore": resource.MustParse("4"),
				},
			},
			expected: `ray start --head  --resources='{"neuron_cores":4}' `,
		},
		{
			name:           "HeadNode with multiple accelerators",
			nodeType:       rayv1.HeadNode,
			rayStartParams: map[string]string{},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"aws.amazon.com/neuroncore": resource.MustParse("4"),
					"nvidia.com/gpu":            resource.MustParse("1"),
				},
			},
			expected: `ray start --head  --num-gpus=1  --resources='{"neuron_cores":4}' `,
		},
		{
			name:           "HeadNode with multiple custom accelerators",
			nodeType:       rayv1.HeadNode,
			rayStartParams: map[string]string{},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"google.com/tpu":            resource.MustParse("8"),
					"aws.amazon.com/neuroncore": resource.MustParse("4"),
					"nvidia.com/gpu":            resource.MustParse("1"),
				},
			},
			expected: `ray start --head  --num-gpus=1  --resources='{"neuron_cores":4}' `,
		},
		{
			name:     "HeadNode with existing resources",
			nodeType: rayv1.HeadNode,
			rayStartParams: map[string]string{
				"resources": `"{"custom_resource":2}"`,
			},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"aws.amazon.com/neuroncore": resource.MustParse("4"),
				},
			},
			expected: `ray start --head  --resources='{"custom_resource":2,"neuron_cores":4}' `,
		},
		{
			name:     "HeadNode with existing neuron_cores resources",
			nodeType: rayv1.HeadNode,
			rayStartParams: map[string]string{
				"resources": `'{"custom_resource":2,"neuron_cores":3}'`,
			},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"aws.amazon.com/neuroncore": resource.MustParse("4"),
				},
			},
			expected: `ray start --head  --resources='{"custom_resource":2,"neuron_cores":3}' `,
		},
		{
			name:     "HeadNode with existing TPU resources",
			nodeType: rayv1.HeadNode,
			rayStartParams: map[string]string{
				"resources": `'{"custom_resource":2,"TPU":4}'`,
			},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"google.com/tpu": resource.MustParse("8"),
				},
			},
			expected: `ray start --head  --resources='{"custom_resource":2,"TPU":4}' `,
		},
		{
			name:     "HeadNode with invalid resources string",
			nodeType: rayv1.HeadNode,
			rayStartParams: map[string]string{
				"resources": "{",
			},
			resource: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"aws.amazon.com/neuroncore": resource.MustParse("4"),
				},
			},
			expected: "ray start --head  --resources={ ",
		},
		{
			name:           "Invalid node type",
			nodeType:       "InvalidType",
			rayStartParams: map[string]string{},
			resource:       corev1.ResourceRequirements{},
			expected:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateRayStartCommand(context.TODO(), tt.nodeType, tt.rayStartParams, tt.resource)
			assert.Equal(t, tt.expected, result)
		})
	}
}
