package model

import (
	"reflect"
	"testing"

	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	enableIngress          = false
	headNodeReplicas int32 = 1
	workerReplicas   int32 = 5
)

var headSpecTest = v1alpha1.HeadGroupSpec{
	ServiceType:   "ClusterIP",
	EnableIngress: &enableIngress,
	Replicas:      &headNodeReplicas,
	RayStartParams: map[string]string{
		"dashboard-host":      "0.0.0.0",
		"metrics-export-port": "8080",
		"num-cpus":            "0",
	},
	Template: v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"openshift.io/scc":    "restricted",
				"ray.io/ft-enabled":   "false",
				"ray.io/health-state": "",
				"custom":              "value",
			},
			Labels: map[string]string{
				"app.kubernetes.io/created-by": "kuberay-operator",
				"app.kubernetes.io/name":       "kuberay",
				"ray.io/cluster":               "boris-cluster",
				"ray.io/cluster-dashboard":     "boris-cluster-dashboard",
				"ray.io/group":                 "headgroup",
				"ray.io/identifier":            "boris-cluster-head",
				"ray.io/is-ray-node":           "yes",
				"ray.io/node-type":             "head",
				"test":                         "value",
			},
			Name:      "boris-cluster-head-f7zx2",
			Namespace: "max",
		},
		Spec: v1.PodSpec{
			ServiceAccountName: "account",
			Tolerations: []v1.Toleration{
				{
					Key:      "blah1",
					Operator: "Exists",
					Effect:   "NoExecute",
				},
			},
			Containers: []v1.Container{
				{
					Name:  "ray-head",
					Image: "blublinsky1/ray310:2.4.0",
					Env: []v1.EnvVar{
						{
							Name:  "AWS_KEY",
							Value: "123",
						},
						{
							Name:  "AWS_SECRET",
							Value: "1234",
						},
						{
							Name:  "RAY_PORT",
							Value: "6379",
						},
						{
							Name:  "RAY_ADDRESS",
							Value: "127.0.0.1:6379",
						},
						{
							Name:  "RAY_USAGE_STATS_KUBERAY_IN_USE",
							Value: "1",
						},
					},
				},
			},
		},
	},
}

var configMapWithoutTolerations = v1.ConfigMap{
	Data: map[string]string{
		"cpu":             "4",
		"gpu":             "0",
		"gpu_accelerator": "",
		"memory":          "8",
		"name":            "head-node-template",
		"namespace":       "max",
	},
}

var configMapWithTolerations = v1.ConfigMap{
	Data: map[string]string{
		"cpu":             "4",
		"gpu":             "0",
		"gpu_accelerator": "",
		"memory":          "8",
		"name":            "head-node-template",
		"namespace":       "max",
		"tolerations":     "[{\"key\":\"blah1\",\"operator\":\"Exists\",\"effect\":\"NoExecute\"}]",
	},
}

var workerSpecTest = v1alpha1.WorkerGroupSpec{
	GroupName:   "",
	Replicas:    &workerReplicas,
	MinReplicas: &workerReplicas,
	MaxReplicas: &workerReplicas,
	RayStartParams: map[string]string{
		"node-ip-address": "$MY_POD_IP",
	},
	Template: v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"cni.projectcalico.org/containerID": "cce862a899455385e98e3453ba9ef5a376e85ad45c3e95b18e04e001204af728",
				"cni.projectcalico.org/podIP":       "172.17.60.2/32",
				"cni.projectcalico.org/podIPs":      "172.17.60.2/32",
				"openshift.io/scc":                  "restricted",
				"ray.io/ft-enabled":                 "false",
				"ray.io/health-state":               "",
				"custom":                            "value",
			},
			Labels: map[string]string{
				"app.kubernetes.io/created-by": "kuberay-operator",
				"app.kubernetes.io/name":       "kuberay",
				"ray.io/cluster":               "boris-cluster",
				"ray.io/cluster-dashboard":     "boris-cluster-dashboard",
				"ray.io/group":                 "8-CPUs",
				"ray.io/identifier":            "boris-cluster-worker",
				"ray.io/is-ray-node":           "yes",
				"ray.io/node-type":             "worker",
				"test":                         "value",
			},
			Name:      "boris-cluster-worker-8-cpus-4dp9v",
			Namespace: "max",
		},
		Spec: v1.PodSpec{
			ServiceAccountName: "account",
			Tolerations: []v1.Toleration{
				{
					Key:      "blah1",
					Operator: "Exists",
					Effect:   "NoExecute",
				},
			},
			Containers: []v1.Container{
				{
					Name:  "ray-worker",
					Image: "blublinsky1/ray310:2.4.0",
					Env: []v1.EnvVar{
						{
							Name:  "AWS_KEY",
							Value: "123",
						},
						{
							Name:  "AWS_SECRET",
							Value: "1234",
						},
						{
							Name:  "RAY_DISABLE_DOCKER_CPU_WARNING",
							Value: "1",
						},
						{
							Name:  "TYPE",
							Value: "worker",
						},
						{
							Name:  "RAY_IP",
							Value: "boris-cluster-head-svc",
						},
						{
							Name:  "RAY_USAGE_STATS_KUBERAY_IN_USE",
							Value: "1",
						},
					},
				},
			},
		},
	},
}

var expectedAnnotations = map[string]string{
	"custom": "value",
}

var expectedLabels = map[string]string{
	"test": "value",
}

var expectedEnv = map[string]string{
	"AWS_KEY":    "123",
	"AWS_SECRET": "1234",
}

var expectedTolerations = api.PodToleration{
	Key:      "blah1",
	Operator: "Exists",
	Effect:   "NoExecute",
}

func TestPopulateHeadNodeSpec(t *testing.T) {
	groupSpec := PopulateHeadNodeSpec(headSpecTest)

	if groupSpec.ServiceAccount != "account" {
		t.Errorf("failed to convert service account")
	}
	if !reflect.DeepEqual(groupSpec.Annotations, expectedAnnotations) {
		t.Errorf("failed to convert annotations, got %v, expected %v", groupSpec.Annotations, expectedAnnotations)
	}
	if !reflect.DeepEqual(groupSpec.Labels, expectedLabels) {
		t.Errorf("failed to convert labels, got %v, expected %v", groupSpec.Labels, expectedLabels)
	}
	if !reflect.DeepEqual(groupSpec.Environment, expectedEnv) {
		t.Errorf("failed to convert annotations, got %v, expected %v", groupSpec.Environment, expectedEnv)
	}
}

func TestPopulateWorkerNodeSpec(t *testing.T) {
	groupSpec := PopulateWorkerNodeSpec([]v1alpha1.WorkerGroupSpec{workerSpecTest})[0]

	if groupSpec.ServiceAccount != "account" {
		t.Errorf("failed to convert service account")
	}
	if !reflect.DeepEqual(groupSpec.Annotations, expectedAnnotations) {
		t.Errorf("failed to convert annotations, got %v, expected %v", groupSpec.Annotations, expectedAnnotations)
	}
	if !reflect.DeepEqual(groupSpec.Labels, expectedLabels) {
		t.Errorf("failed to convert labels, got %v, expected %v", groupSpec.Labels, expectedLabels)
	}
	if !reflect.DeepEqual(groupSpec.Environment, expectedEnv) {
		t.Errorf("failed to convert annotations, got %v, expected %v", groupSpec.Environment, expectedEnv)
	}
}

func TestPopulateTemplate(t *testing.T) {
	template := FromKubeToAPIComputeTemplate(&configMapWithoutTolerations)
	if len(template.Tolerations) != 0 {
		t.Errorf("failed to convert config map, expected no tolerations, got %d", len(template.Tolerations))
	}

	template = FromKubeToAPIComputeTemplate(&configMapWithTolerations)
	if len(template.Tolerations) != 1 {
		t.Errorf("failed to convert config map, expected 1 toleration, got %d", len(template.Tolerations))
	}
	if template.Tolerations[0].Key != expectedTolerations.Key ||
		template.Tolerations[0].Operator != expectedTolerations.Operator ||
		template.Tolerations[0].Effect != expectedTolerations.Effect {
		t.Errorf("failed to convert config map, got %v, expected %v", tolerationToString(template.Tolerations[0]),
			tolerationToString(&expectedTolerations))
	}
}

func tolerationToString(toleration *api.PodToleration) string {
	return "Key: " + toleration.Key + " Operator: " + string(toleration.Operator) + " Effect: " + string(toleration.Effect)
}
