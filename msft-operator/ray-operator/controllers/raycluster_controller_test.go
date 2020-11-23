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

package controllers

import (
	"context"
	"fmt"
	rayiov1alpha1 "ray-operator/api/v1alpha1"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	SetupTest(ctx)
	var workerPods corev1.PodList

	var myRayCluster = &rayiov1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayClusterSpec{
			RayVersion: "1.0",
			HeadService: v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "head-svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "redis", Port: int32(6379)}},
					// Use a headless service, meaning that the DNS record for the service will
					// point directly to the head node pod's IP address.
					ClusterIP: corev1.ClusterIPNone,
					// This selector must match the label of the head node.
					Selector: map[string]string{
						"identifier": "raycluster-sample-head",
					},
				},
			},
			HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
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
							corev1.Container{
								Name:    "ray-head",
								Image:   "rayproject/autoscaler",
								Command: []string{"python"},
								Args:    []string{"/opt/code.py"},
								Env: []corev1.EnvVar{
									corev1.EnvVar{
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
			WorkerGroupsSpec: []rayiov1alpha1.WorkerGroupSpec{
				rayiov1alpha1.WorkerGroupSpec{
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
								corev1.Container{
									Name:    "ray-worker",
									Image:   "rayproject/autoscaler",
									Command: []string{"echo"},
									Args:    []string{"Hello Ray"},
									Env: []corev1.EnvVar{
										corev1.EnvVar{
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
	}

	Describe("When creating a raycluster", func() {

		It("should create a raycluster object", func() {
			err := K8sClient.Create(ctx, myRayCluster)
			Expect(err).NotTo(HaveOccurred(), "failed to create test RayCluster resource")
		})

		It("should see the a raycluster object", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayCluster.Name, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
		})

		It("should create a new head service resource", func() {
			svc := &corev1.Service{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: "raycluster-sample-head-svc", Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My head service = %v", svc)
			Expect(svc.Spec.Selector["identifier"]).Should(Equal(fmt.Sprintf("%s-%s", myRayCluster.Name, rayiov1alpha1.HeadNode)))
		})

		It("should create more than 1 worker", func() {

			K8sClient.List(context.TODO(), &workerPods,
				client.InNamespace(myRayCluster.Namespace), client.MatchingLabels{"rayClusterName": myRayCluster.Name, "groupName": "small-group"}, &client.ListOptions{Namespace: "default"})
			Expect(len(workerPods.Items)).Should(BeNumerically("==", 3), "My pod list= %v", workerPods.Items)
			if len(workerPods.Items) > 0 {
				Expect(workerPods.Items[0].Status.Phase).Should((Or(Equal(v1.PodRunning), Equal(v1.PodPending))))
			}
		})

		It("should create a head pod resource", func() {
			var headPods corev1.PodList

			K8sClient.List(context.TODO(), &headPods,
				client.InNamespace(myRayCluster.Namespace),
				client.MatchingLabels{"rayClusterName": myRayCluster.Name, "groupName": "headgroup"}, &client.ListOptions{Namespace: "default"})

			Expect(len(headPods.Items)).Should(BeNumerically("==", 1), "My head pod list= %v", headPods.Items)

			pod := &corev1.Pod{}
			if len(headPods.Items) > 0 {
				pod = &headPods.Items[0]
			}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: pod.Name, Namespace: "default"}, pod),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My head pod = %v", pod)
			Expect(pod.Status.Phase).Should(Or(Equal(v1.PodPending), Equal(v1.PodRunning)))

		})

		It("should re-create a deleted worker", func() {
			//var podList corev1.PodList
			pod := workerPods.Items[0]
			K8sClient.List(context.TODO(), &workerPods,
				client.InNamespace(myRayCluster.Namespace), client.MatchingLabels{"rayClusterName": myRayCluster.Name, "groupName": "small-group"}, &client.ListOptions{Namespace: "default"})
			Expect(len(workerPods.Items)).Should(BeNumerically("==", 3), "My pod list= %v", workerPods.Items)

			err := K8sClient.Delete(context.Background(), &pod,
				&client.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(0)})

			Expect(err).NotTo(HaveOccurred(), "failed delete a pod")

			Eventually(
				listResourceFunc(context.Background(), &workerPods, &client.ListOptions{Namespace: "default"}),
				time.Second*25, time.Millisecond*500).Should(BeNil(), "My pod list= %v", workerPods)

			//at least 3 pods should be in none-failed phase
			count := 0
			for _, aPod := range workerPods.Items {
				if reflect.DeepEqual(aPod.Status.Phase, v1.PodRunning) || reflect.DeepEqual(aPod.Status.Phase, v1.PodPending) {
					count++
				}
			}
			Expect(count).Should(BeNumerically("==", 3))

		})

		It("should update a raycluster object", func() {
			// adding a scale strategy
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayCluster.Name, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My raycluster = %v", myRayCluster)

			podToDelete1 := workerPods.Items[0]
			rep := new(int32)
			*rep = 2
			myRayCluster.Spec.WorkerGroupsSpec[0].Replicas = rep
			myRayCluster.Spec.WorkerGroupsSpec[0].ScaleStrategy.WorkersToDelete = []string{podToDelete1.Name}

			err := K8sClient.Update(ctx, myRayCluster)
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayCluster resource")
		})
		It("should have only 2 running worker", func() {

			K8sClient.List(context.TODO(), &workerPods,
				client.InNamespace(myRayCluster.Namespace), client.MatchingLabels{"rayClusterName": myRayCluster.Name, "groupName": "small-group"}, &client.ListOptions{Namespace: "default"})
			count := 0
			for _, aPod := range workerPods.Items {
				if reflect.DeepEqual(aPod.Status.Phase, v1.PodRunning) || reflect.DeepEqual(aPod.Status.Phase, v1.PodPending) {
					count++
				}
			}
			Expect(count).Should(BeNumerically("==", 2), fmt.Sprintf("worker pod %v", workerPods.Items))
		})
	})
})

func getResourceFunc(ctx context.Context, key client.ObjectKey, obj runtime.Object) func() error {
	return func() error {
		return K8sClient.Get(ctx, key, obj)
	}
}

func listResourceFunc(ctx context.Context, list runtime.Object, opt client.ListOption) func() error {
	return func() error {
		return K8sClient.List(ctx, list, opt)
	}
}
