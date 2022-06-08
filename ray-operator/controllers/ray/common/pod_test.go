package common

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var testMemoryLimit = resource.MustParse("1Gi")

var instance = rayiov1alpha1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayiov1alpha1.RayClusterSpec{
		RayVersion: "12.0.1",
		HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
			ServiceType: "ClusterIP",
			Replicas:    pointer.Int32Ptr(1),
			RayStartParams: map[string]string{
				"port":                "6379",
				"object-manager-port": "12345",
				"node-manager-port":   "12346",
				"object-store-memory": "100000000",
				"redis-password":      "LetMeInRay",
				"num-cpus":            "1",
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						"ray.io/cluster": "raycluster-sample",
						"ray.io/group":   "headgroup",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "ray-head",
							Image: "rayproject/ray:12.0.1",
							Env: []v1.EnvVar{
								{
									Name: "MY_POD_IP",
									ValueFrom: &v1.EnvVarSource{
										FieldRef: &v1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
									},
								},
							},
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: testMemoryLimit,
								},
								Limits: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: testMemoryLimit,
								},
							},
						},
					},
				},
			},
		},
		WorkerGroupSpecs: []rayiov1alpha1.WorkerGroupSpec{
			{
				Replicas:    pointer.Int32Ptr(3),
				MinReplicas: pointer.Int32Ptr(0),
				MaxReplicas: pointer.Int32Ptr(10000),
				GroupName:   "small-group",
				RayStartParams: map[string]string{
					"port":           "6379",
					"redis-password": "LetMeInRay",
					"num-cpus":       "1",
					"block":          "true",
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Labels: map[string]string{
							"ray.io/cluster": "raycluster-sample",
							"ray.io/group":   "small-group",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "ray-worker",
								Image: "rayproject/autoscaler",
								Env: []v1.EnvVar{
									{
										Name: "MY_POD_IP",
										ValueFrom: &v1.EnvVarSource{
											FieldRef: &v1.ObjectFieldSelector{
												FieldPath: "status.podIP",
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
	},
}

var volumesNoAutoscaler = []v1.Volume{
	{
		Name: "shared-mem",
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium:    v1.StorageMediumMemory,
				SizeLimit: &testMemoryLimit,
			},
		},
	},
}

var volumesWithAutoscaler = []v1.Volume{
	{
		Name: "shared-mem",
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium:    v1.StorageMediumMemory,
				SizeLimit: &testMemoryLimit,
			},
		},
	},
	{
		Name: "ray-logs",
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium: v1.StorageMediumDefault,
			},
		},
	},
}

var volumeMountsNoAutoscaler = []v1.VolumeMount{
	{
		Name:      "shared-mem",
		MountPath: "/dev/shm",
		ReadOnly:  false,
	},
}

var volumeMountsWithAutoscaler = []v1.VolumeMount{
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

var autoscalerContainer = v1.Container{
	Name:            "autoscaler",
	Image:           "rayproject/ray:448f52",
	ImagePullPolicy: v1.PullAlways,
	Env: []v1.EnvVar{
		{
			Name: "RAY_CLUSTER_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "metadata.labels['ray.io/cluster']",
				},
			},
		},
		{
			Name: "RAY_CLUSTER_NAMESPACE",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	},
	Command: []string{
		"ray",
	},
	Args: []string{
		"kuberay-autoscaler",
		"--cluster-name",
		"$(RAY_CLUSTER_NAME)",
		"--cluster-namespace",
		"$(RAY_CLUSTER_NAMESPACE)",
	},
	Resources: v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("500m"),
			v1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("256m"),
			v1.ResourceMemory: resource.MustParse("256Mi"),
		},
	},
	VolumeMounts: []v1.VolumeMount{
		{
			MountPath: "/tmp/ray",
			Name:      "ray-logs",
		},
	},
}

var trueFlag = true

func TestBuildPod(t *testing.T) {
	cluster := instance.DeepCopy()

	// Test head pod
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName)
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName, nil)

	actualResult := pod.Labels[RayClusterLabelKey]
	expectedResult := cluster.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeTypeLabelKey]
	expectedResult = string(rayiov1alpha1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeGroupLabelKey]
	expectedResult = "headgroup"
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualVolumes := pod.Spec.Volumes
	expectedVolumes := volumesNoAutoscaler
	if !reflect.DeepEqual(actualVolumes, expectedVolumes) {
		t.Fatalf("Expected `%v` but got `%v`", expectedVolumes, actualVolumes)
	}

	actualVolumeMounts := pod.Spec.Containers[0].VolumeMounts
	expectedVolumeMounts := volumeMountsNoAutoscaler
	if !reflect.DeepEqual(actualVolumeMounts, expectedVolumeMounts) {
		t.Fatalf("Expected `%v` but got `%v`", expectedVolumeMounts, actualVolumeMounts)
	}

	// testing worker pod
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + DashSymbol + string(rayiov1alpha1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	podTemplateSpec = DefaultWorkerPodTemplate(*cluster, worker, podName, svcName)
	pod = BuildPod(podTemplateSpec, rayiov1alpha1.WorkerNode, worker.RayStartParams, svcName, nil)

	expectedResult = fmt.Sprintf("%s:6379", svcName)
	actualResult = cluster.Spec.WorkerGroupSpecs[0].RayStartParams["address"]

	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	expectedCommandArg := splitAndSort("ulimit -n 65536; ray start --block --num-cpus=1 --address=raycluster-sample-head-svc:6379 --port=6379 --redis-password=LetMeInRay --metrics-export-port=8080")
	if !reflect.DeepEqual(expectedCommandArg, splitAndSort(pod.Spec.Containers[0].Args[0])) {
		t.Fatalf("Expected `%v` but got `%v`", expectedCommandArg, pod.Spec.Containers[0].Args[0])
	}
}

func TestBuildPod_WithAutoscalerEnabled(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName)
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName, &trueFlag)

	actualResult := pod.Labels[RayClusterLabelKey]
	expectedResult := cluster.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeTypeLabelKey]
	expectedResult = string(rayiov1alpha1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeGroupLabelKey]
	expectedResult = "headgroup"
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

// Check that autoscaler container overrides work as expected.
func TestBuildPodWithAutoscalerOptions(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)

	customAutoscalerImage := "custom-autoscaler-xxx"
	customPullPolicy := v1.PullIfNotPresent
	customTimeout := int32(100)
	customUpscaling := rayiov1alpha1.UpscalingMode("Aggressive")
	customResources := v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1"),
			v1.ResourceMemory: testMemoryLimit,
		},
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1"),
			v1.ResourceMemory: testMemoryLimit,
		},
	}

	cluster.Spec.AutoscalerOptions = &rayiov1alpha1.AutoscalerOptions{
		UpscalingMode:      &customUpscaling,
		IdleTimeoutSeconds: &customTimeout,
		Image:              &customAutoscalerImage,
		ImagePullPolicy:    &customPullPolicy,
		Resources:          &customResources,
	}
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName)
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName, &trueFlag)
	expectedContainer := *autoscalerContainer.DeepCopy()
	expectedContainer.Image = customAutoscalerImage
	expectedContainer.ImagePullPolicy = customPullPolicy
	expectedContainer.Resources = customResources
	index := getAutoscalerContainerIndex(pod)
	actualContainer := pod.Spec.Containers[index]
	if !reflect.DeepEqual(expectedContainer, actualContainer) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainer, actualContainer)
	}
}

func TestDefaultHeadPodTemplate_WithAutoscalingEnabled(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName)

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
}

// If no service account is specified in the RayCluster,
// the head pod's service account should be an empty string.
func TestHeadPodTemplate_WithNoServiceAccount(t *testing.T) {
	cluster := instance.DeepCopy()
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName)

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
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName)

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
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName)

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
