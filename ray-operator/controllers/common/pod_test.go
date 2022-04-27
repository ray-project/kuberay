package common

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/utils"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var instance = rayiov1alpha1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayiov1alpha1.RayClusterSpec{
		RayVersion: "1.0.0",
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

var autoscalerContainer = v1.Container{
	Name:            "autoscaler",
	Image:           "kuberay/autoscaler:nightly",
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
		"/home/ray/anaconda3/bin/python",
	},
	Args: []string{
		"/home/ray/run_autoscaler_with_retries.py",
		"--cluster-name",
		"$(RAY_CLUSTER_NAME)",
		"--cluster-namespace",
		"$(RAY_CLUSTER_NAMESPACE)",
		"--redis-password",
		DefaultRedisPassword,
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
}

var trueFlag = true

func TestBuildPod(t *testing.T) {
	cluster := instance.DeepCopy()
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName)
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName)

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

	// testing worker pod
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + DashSymbol + string(rayiov1alpha1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	podTemplateSpec = DefaultWorkerPodTemplate(*cluster, worker, podName, svcName)
	pod = BuildPod(podTemplateSpec, rayiov1alpha1.WorkerNode, worker.RayStartParams, svcName)

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
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName)

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

	actualResult = cluster.Spec.HeadGroupSpec.RayStartParams["redis-password"]
	targetContainer, err := utils.FilterContainerByName(pod.Spec.Containers, "autoscaler")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !utils.Contains(targetContainer.Args, actualResult) {
		t.Fatalf("Expected redis password `%v` in `%v` but not found", targetContainer.Args, actualResult)
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

func TestBuildAutoscalerContainer(t *testing.T) {
	actualContainer := BuildAutoscalerContainer(DefaultRedisPassword)
	expectedContainer := autoscalerContainer
	if !reflect.DeepEqual(expectedContainer, actualContainer) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainer, actualContainer)
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
