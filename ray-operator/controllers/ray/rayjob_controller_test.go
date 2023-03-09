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
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

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

	myRayJob := &rayiov1alpha1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayjob-test",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayJobSpec{
			Entrypoint: "sleep 999",
			RayClusterSpec: &rayiov1alpha1.RayClusterSpec{
				RayVersion: "1.12.1",
				HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
					ServiceType: corev1.ServiceTypeClusterIP,
					Replicas:    pointer.Int32(1),
					RayStartParams: map[string]string{
						"port":                        "6379",
						"object-store-memory":         "100000000",
						"dashboard-host":              "0.0.0.0",
						"num-cpus":                    "1",
						"node-ip-address":             "127.0.0.1",
						"block":                       "true",
						"dashboard-agent-listen-port": "52365",
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"rayCluster": "raycluster-sample",
								"groupName":  "headgroup",
							},
							Annotations: map[string]string{
								"key": "value",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: "rayproject/ray:2.3.0",
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
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1"),
											corev1.ResourceMemory: resource.MustParse("2Gi"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1"),
											corev1.ResourceMemory: resource.MustParse("2Gi"),
										},
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          "gcs-server",
											ContainerPort: 6379,
										},
										{
											Name:          "dashboard",
											ContainerPort: 8265,
										},
										{
											Name:          "head",
											ContainerPort: 10001,
										},
										{
											Name:          "dashboard-agent",
											ContainerPort: 52365,
										},
									},
								},
							},
						},
					},
				},
				WorkerGroupSpecs: []rayiov1alpha1.WorkerGroupSpec{
					{
						Replicas:    pointer.Int32(3),
						MinReplicas: pointer.Int32(0),
						MaxReplicas: pointer.Int32(10000),
						GroupName:   "small-group",
						RayStartParams: map[string]string{
							"port":                        "6379",
							"num-cpus":                    "1",
							"dashboard-agent-listen-port": "52365",
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
										Image:   "rayproject/ray:2.3.0",
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
										Ports: []corev1.ContainerPort{
											{
												Name:          "client",
												ContainerPort: 80,
											},
											{
												Name:          "dashboard-agent",
												ContainerPort: 52365,
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

	myRayCluster := &rayiov1alpha1.RayCluster{}

	myRayJobWithClusterSelector := &rayiov1alpha1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayjob-test-2",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayJobSpec{
			Entrypoint:      "sleep 999",
			ClusterSelector: map[string]string{},
		},
	}

	Describe("When creating a rayjob", func() {
		It("should create a rayjob object", func() {
			err := k8sClient.Create(ctx, myRayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to create test RayJob resource")
		})

		It("should see a rayjob object", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Name, Namespace: "default"}, myRayJob),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayJob  = %v", myRayJob.Name)
		})

		It("should create a raycluster object", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayJob.Status.RayClusterName)

			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
		})

		It("should create more than 1 worker", func() {
			filterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayJob.Status.RayClusterName, common.RayNodeGroupLabelKey: "small-group"}
			Eventually(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))
			if len(workerPods.Items) > 0 {
				Expect(workerPods.Items[0].Status.Phase).Should(Or(Equal(corev1.PodRunning), Equal(corev1.PodPending)))
			}
		})

		It("Dashboard URL should be set", func() {
			Eventually(
				getDashboardURLForRayJob(ctx, myRayJob),
				time.Second*3, time.Millisecond*500).Should(HavePrefix(myRayJob.Name), "Dashboard URL = %v", myRayJob.Status.DashboardURL)
		})

		It("test cluster selector", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayJob.Status.RayClusterName)

			myRayJobWithClusterSelector.Spec.ClusterSelector[RayJobDefaultClusterSelectorKey] = myRayJob.Status.RayClusterName

			err := k8sClient.Create(ctx, myRayJobWithClusterSelector)
			Expect(err).NotTo(HaveOccurred(), "failed to create RayJob resource")

			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJobWithClusterSelector),
				time.Second*15, time.Millisecond*500).Should(Equal(myRayJob.Status.RayClusterName))
		})
	})
})

func getRayClusterNameForRayJob(ctx context.Context, rayJob *rayiov1alpha1.RayJob) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.RayClusterName, nil
	}
}

func getDashboardURLForRayJob(ctx context.Context, rayJob *rayiov1alpha1.RayJob) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.DashboardURL, nil
	}
}
