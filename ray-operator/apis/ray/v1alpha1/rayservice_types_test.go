package v1alpha1

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var numReplicas int32
var numCpus float64

var myRayService = &RayService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rayservice-sample",
		Namespace: "default",
	},
	Spec: RayServiceSpec{
		ServeConfigSpecs: []ServeConfigSpec{
			{
				Name:        "shallow",
				ImportPath:  "test_env.shallow_import.ShallowClass",
				NumReplicas: &numReplicas,
				RoutePrefix: "/shallow",
				RayActorOptions: RayActorOptionSpec{
					NumCpus: &numCpus,
					RuntimeEnv: map[string][]string{
						"py_modules": {
							"https://github.com/shrekris-anyscale/test_deploy_group/archive/HEAD.zip",
							"https://github.com/shrekris-anyscale/test_module/archive/HEAD.zip",
						},
					},
				},
			},
			{
				Name:        "deep",
				ImportPath:  "test_env.subdir1.subdir2.deep_import.DeepClass",
				NumReplicas: &numReplicas,
				RoutePrefix: "/deep",
				RayActorOptions: RayActorOptionSpec{
					NumCpus: &numCpus,
				},
			},
		},
		RayClusterSpec: RayClusterSpec{
			RayVersion: "1.0",
			HeadGroupSpec: HeadGroupSpec{
				Replicas: pointer.Int32Ptr(1),
				RayStartParams: map[string]string{
					"port":                "6379",
					"object-manager-port": "12345",
					"node-manager-port":   "12346",
					"object-store-memory": "100000000",
					"redis-password":      "LetMeInRay",
					"num-cpus":            "1",
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Labels: map[string]string{
							"rayCluster": "raycluster-sample",
							"groupName":  "headgroup",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "ray-head",
								Image:   "rayproject/autoscaler",
								Command: []string{"python"},
								Args:    []string{"/opt/code.py"},
								Env: []corev1.EnvVar{
									{
										Name: "MY_POD_IP",
										ValueFrom: &corev1.EnvVarSource{
											FieldRef: &corev1.ObjectFieldSelector{
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
			WorkerGroupSpecs: []WorkerGroupSpec{
				{
					Replicas:    pointer.Int32Ptr(3),
					MinReplicas: pointer.Int32Ptr(0),
					MaxReplicas: pointer.Int32Ptr(10000),
					GroupName:   "small-group",
					RayStartParams: map[string]string{
						"port":           "6379",
						"redis-password": "LetMeInRay",
						"num-cpus":       "1",
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Labels: map[string]string{
								"rayCluster": "raycluster-sample",
								"groupName":  "small-group",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "ray-worker",
									Image:   "rayproject/autoscaler",
									Command: []string{"echo"},
									Args:    []string{"Hello Ray"},
									Env: []corev1.EnvVar{
										{
											Name: "MY_POD_IP",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
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
	},
}

func TestMarshallingRayService(t *testing.T) {
	numReplicas = 1
	numCpus = 0.1
	// marshal successfully
	_, err := json.Marshal(&myRayService)
	if err != nil {
		t.Fatalf("Expected `%v` but got `%v`", nil, err)
	}
}
