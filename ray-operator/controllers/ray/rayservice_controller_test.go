/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ray

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	var workerPods corev1.PodList
	var enableInTreeAutoscaling = true

	var numReplicas int32
	var numCpus float64
	numReplicas = 1
	numCpus = 0.1

	myRayService := &rayiov1alpha1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayServiceSpec{
			ServeConfigSpecs: []rayiov1alpha1.ServeConfigSpec{
				{
					Name:        "shallow",
					ImportPath:  "test_env.shallow_import.ShallowClass",
					NumReplicas: &numReplicas,
					RoutePrefix: "/shallow",
					RayActorOptions: rayiov1alpha1.RayActorOptionSpec{
						NumCpus: &numCpus,
						RuntimeEnv: map[string][]string{
							"py_modules": {
								"https://github.com/shrekris-anyscale/test_deploy_group/archive/HEAD.zip",
								"https://github.com/shrekris-anyscale/test_module/archive/HEAD.zip",
							},
						},
					},
				},
			},
			RayClusterSpec: rayiov1alpha1.RayClusterSpec{
				RayVersion:              "1.0",
				EnableInTreeAutoscaling: &enableInTreeAutoscaling,
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
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							ServiceAccountName: "head-service-account",
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
						},
						Template: corev1.PodTemplateSpec{
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

	Describe("When creating a rayservice", func() {
		It("should create a rayservice object", func() {
			err := k8sClient.Create(ctx, myRayService)
			Expect(err).NotTo(HaveOccurred(), "failed to create test RayService resource")
		})

		It("should see a rayservice object", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
		})

		It("should see one serve deployment", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
				time.Second*30, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
			Expect(len(myRayService.Status.ServeStatuses), 1)
			Expect(myRayService.Status.ServeStatuses[0].Name, "shallow")
		})

		It("should update a rayservice object", func() {
			// adding a scale strategy
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)

			podToDelete1 := workerPods.Items[0]
			rep := new(int32)
			*rep = 1
			myRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas = rep
			myRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{podToDelete1.Name}

			Expect(k8sClient.Update(ctx, myRayService)).Should(Succeed(), "failed to update test RayService resource")
		})
	})
})
