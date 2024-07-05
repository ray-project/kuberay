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
	"os"
	"time"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	var workerPods corev1.PodList

	testServeAppName := "app1"
	testServeConfigV2 := fmt.Sprintf(`
    applications:
    - name: %s
      import_path: fruit.deployment_graph
      route_prefix: /fruit
      runtime_env:
        working_dir: "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
      deployments:
        - name: MangoStand
          num_replicas: 1
          user_config:
            price: 3
          ray_actor_options:
            num_cpus: 0.1
        - name: OrangeStand
          num_replicas: 1
          user_config:
            price: 2
          ray_actor_options:
            num_cpus: 0.1
        - name: PearStand
          num_replicas: 1
          user_config:
            price: 1
          ray_actor_options:
            num_cpus: 0.1`, testServeAppName)

	myRayService := &rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayservice-sample",
			Namespace: "default",
		},
		Spec: rayv1.RayServiceSpec{
			ServeConfigV2: testServeConfigV2,
			RayClusterSpec: rayv1.RayClusterSpec{
				RayVersion: "2.9.0",
				HeadGroupSpec: rayv1.HeadGroupSpec{
					RayStartParams: map[string]string{
						"port":                        "6379",
						"object-store-memory":         "100000000",
						"dashboard-host":              "0.0.0.0",
						"num-cpus":                    "1",
						"node-ip-address":             "127.0.0.1",
						"dashboard-agent-listen-port": "52365",
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"groupName": utils.RayNodeHeadGroupLabelValue,
							},
							Annotations: map[string]string{
								"key": "value",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: "rayproject/ray:2.9.0",
									Env: []corev1.EnvVar{
										{
											Name: "MY_POD_IP",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													FieldPath: "status.podIP",
												},
											},
										},
										{
											Name:  "SAMPLE_ENV_VAR",
											Value: "SAMPLE_VALUE",
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
										{
											Name:          "serve",
											ContainerPort: 8000,
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
							"port":                        "6379",
							"num-cpus":                    "1",
							"dashboard-agent-listen-port": "52365",
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Labels: map[string]string{
									"groupName": "small-group",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:    "ray-worker",
										Image:   "rayproject/ray:2.9.0",
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
											{
												Name:  "SAMPLE_ENV_VAR",
												Value: "SAMPLE_VALUE",
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

	myRayCluster := &rayv1.RayCluster{}

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

		It("should create a raycluster object", func() {
			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "Pending RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)
			pendingRayClusterName := myRayService.Status.PendingServiceStatus.RayClusterName

			// Update the status of the head Pod to Running.
			updateHeadPodToRunningAndReady(ctx, pendingRayClusterName, "default")

			// Make sure the pending RayCluster becomes the active RayCluster.
			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Equal(pendingRayClusterName), "Active RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			// Initialize myRayCluster for the following tests.
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Status.ActiveServiceStatus.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "myRayCluster  = %v", myRayCluster.Name)
		})

		It("should create more than 1 worker", func() {
			Eventually(
				listResourceFunc(ctx, &workerPods, common.RayClusterGroupPodsAssociationOptions(myRayCluster, "small-group").ToListOptions()...),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))
			if len(workerPods.Items) > 0 {
				Expect(workerPods.Items[0].Status.Phase).Should(Or(Equal(corev1.PodRunning), Equal(corev1.PodPending)))
				// All the worker Pods should have a port with the name "dashboard-agent"
				for _, pod := range workerPods.Items {
					// Worker Pod should have only one container.
					Expect(pod.Spec.Containers).Should(HaveLen(1))
					Expect(utils.EnvVarExists(utils.RAY_SERVE_KV_TIMEOUT_S, pod.Spec.Containers[utils.RayContainerIndex].Env)).Should(BeTrue())
				}
			}
		})

		It("Dashboard should be healthy", func() {
			Eventually(
				checkServiceHealth(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "My myRayService status = %v", myRayService.Status)
		})

		It("should create a new head service resource", func() {
			svc := &corev1.Service{}
			headSvcName, err := utils.GenerateHeadServiceName(utils.RayServiceCRD, myRayService.Spec.RayClusterSpec, myRayService.Name)
			Expect(err).ToNot(HaveOccurred(), "failed to generate head service name")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: headSvcName, Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My head service = %v", svc)
			Expect(svc.Spec.Selector[utils.RayIDLabelKey]).Should(Equal(utils.GenerateIdentifier(myRayCluster.Name, rayv1.HeadNode)))
		})

		It("should create a new serve service resource", func() {
			svc := &corev1.Service{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: utils.GenerateServeServiceName(myRayService.Name), Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My serve service = %v", svc)
			Expect(svc.Spec.Selector[utils.RayClusterLabelKey]).Should(Equal(myRayCluster.Name))
		})

		It("should update a rayservice object and switch to new Ray Cluster", func() {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)

				myRayService.Spec.RayClusterSpec.RayVersion = "2.100.0"
				return k8sClient.Update(ctx, myRayService)
			})

			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource")

			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "Pending RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)
			pendingRayClusterName := myRayService.Status.PendingServiceStatus.RayClusterName

			// Update the status of the head Pod to Running.
			updateHeadPodToRunningAndReady(ctx, pendingRayClusterName, "default")

			// Confirm switch to a new Ray Cluster.
			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Equal(pendingRayClusterName), "My new RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Status.ActiveServiceStatus.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
		})

		It("Disable zero-downtime upgrade", func() {
			// Disable zero-downtime upgrade.
			os.Setenv("ENABLE_ZERO_DOWNTIME", "false")

			// Try to trigger a zero-downtime upgrade.
			oldRayVersion := myRayService.Spec.RayClusterSpec.RayVersion
			newRayVersion := "2.198.0"
			Expect(oldRayVersion).ShouldNot(Equal(newRayVersion))
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
				myRayService.Spec.RayClusterSpec.RayVersion = newRayVersion
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource")

			// Because the zero-downtime upgrade is disabled, the RayService controller will not prepare a new RayCluster.
			Consistently(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(BeEmpty(), "Pending RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)

			// Set the RayVersion back to the old value to avoid triggering the zero-downtime upgrade.
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
				myRayService.Spec.RayClusterSpec.RayVersion = oldRayVersion
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource")

			// Enable zero-downtime upgrade again.
			os.Unsetenv("ENABLE_ZERO_DOWNTIME")

			// Zero-downtime upgrade should not be triggered.
			Consistently(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(BeEmpty(), "Pending RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)
		})

		It("Autoscaler updates the active RayCluster and should not switch to a new RayCluster", func() {
			// Simulate autoscaler by updating the active RayCluster directly. Note that the autoscaler
			// will not update the RayService directly.
			initialClusterName, _ := getRayClusterNameFunc(ctx, myRayService)()
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: initialClusterName, Namespace: "default"}, myRayCluster),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "Active RayCluster = %v", myRayCluster.Name)
				podToDelete := workerPods.Items[0]
				*myRayCluster.Spec.WorkerGroupSpecs[0].Replicas++
				myRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{podToDelete.Name}
				return k8sClient.Update(ctx, myRayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayCluster")

			// Confirm not switch to a new RayCluster
			Consistently(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(Equal(initialClusterName), "My current RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Status.ActiveServiceStatus.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)

			cleanUpWorkersToDelete(ctx, myRayCluster)
		})

		It("Autoscaler updates the pending RayCluster and should not switch to a new RayCluster", func() {
			// Simulate autoscaler by updating the pending RayCluster directly. Note that the autoscaler
			// will not update the RayService directly.

			// Trigger a new RayCluster preparation by updating the RayVersion.
			oldRayVersion := myRayService.Spec.RayClusterSpec.RayVersion
			newRayVersion := "2.200.0"
			Expect(oldRayVersion).ShouldNot(Equal(newRayVersion))
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
				myRayService.Spec.RayClusterSpec.RayVersion = newRayVersion
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource")
			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*60, time.Millisecond*500).Should(Not(BeEmpty()), "New pending RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)
			initialPendingClusterName, _ := getPreparingRayClusterNameFunc(ctx, myRayService)()

			// Simulate that the pending RayCluster is updated by the autoscaler.
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: initialPendingClusterName, Namespace: "default"}, myRayCluster),
					time.Second*15, time.Millisecond*500).Should(BeNil(), "Pending RayCluster = %v", myRayCluster.Name)
				podToDelete := workerPods.Items[0]
				*myRayCluster.Spec.WorkerGroupSpecs[0].Replicas++
				myRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{podToDelete.Name}
				return k8sClient.Update(ctx, myRayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to update the pending RayCluster.")

			// Confirm not switch to a new RayCluster when the pending RayCluster triggers autoscaler.
			Consistently(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(Equal(initialPendingClusterName), "Pending RayCluster name = %v", myRayService.Status.PendingServiceStatus.RayClusterName)

			// The pending RayCluster will become the active RayCluster after:
			// (1) The pending RayCluster's head Pod becomes Running and Ready
			// (2) The pending RayCluster's Serve Deployments are HEALTHY.
			updateHeadPodToRunningAndReady(ctx, initialPendingClusterName, "default")
			healthyStatus := generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING)
			fakeRayDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{testServeAppName: &healthyStatus})
			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(BeEmpty(), "Pending RayCluster name = %v", myRayService.Status.PendingServiceStatus.RayClusterName)
			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Equal(initialPendingClusterName), "New active RayCluster name = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			cleanUpWorkersToDelete(ctx, myRayCluster)
		})
		It("should update the active RayCluster in place when WorkerGroupSpecs are modified by the user in RayServiceSpec", func() {
			initialClusterName, _ := getRayClusterNameFunc(ctx, myRayService)()
			oldNumWorkerGroupSpecs := len(myRayService.Spec.RayClusterSpec.WorkerGroupSpecs)
			// Add a new worker group to the RayServiceSpec
			newWorkerGroupSpec := myRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].DeepCopy()
			newWorkerGroupSpec.GroupName = "worker-group-2"

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
				myRayService.Spec.RayClusterSpec.WorkerGroupSpecs = append(myRayService.Spec.RayClusterSpec.WorkerGroupSpecs, *newWorkerGroupSpec)
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource")

			// Confirm it didn't switch to a new RayCluster
			Consistently(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(Equal(initialClusterName), "My current RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Status.ActiveServiceStatus.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)

			// Verify that the active RayCluster eventually reflects the changes in WorkerGroupSpecs
			Eventually(
				getActiveRayClusterWorkerGroupSpecsFunc(ctx, myRayService),
				time.Second*15,
				time.Millisecond*500,
			).Should(
				HaveLen(oldNumWorkerGroupSpecs + 1),
			)
		})
		It("should update the pending RayCluster in place when WorkerGroupSpecs are modified by the user in RayServiceSpec", func() {
			// Trigger a new RayCluster preparation by updating the RayVersion.
			oldRayVersion := myRayService.Spec.RayClusterSpec.RayVersion
			newRayVersion := "2.300.0"
			Expect(oldRayVersion).ShouldNot(Equal(newRayVersion))
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
				myRayService.Spec.RayClusterSpec.RayVersion = newRayVersion
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource with new RayVersion")
			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*60, time.Millisecond*500).Should(Not(BeEmpty()), "New pending RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)
			initialPendingClusterName, _ := getPreparingRayClusterNameFunc(ctx, myRayService)()

			// Add a new worker group to the RayServiceSpec
			oldNumWorkerGroupSpecs := len(myRayService.Spec.RayClusterSpec.WorkerGroupSpecs)
			newWorkerGroupSpec := myRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].DeepCopy()
			newWorkerGroupSpec.GroupName = "worker-group-3"

			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
				myRayService.Spec.RayClusterSpec.WorkerGroupSpecs = append(myRayService.Spec.RayClusterSpec.WorkerGroupSpecs, *newWorkerGroupSpec)
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource with new WorkerGroupSpecs")

			// Sanity check: length of myRayService.Spec.RayClusterSpec.WorkerGroupSpecs should be 3
			Expect(myRayService.Spec.RayClusterSpec.WorkerGroupSpecs).Should(HaveLen(oldNumWorkerGroupSpecs + 1))

			// Confirm it didn't switch to a new RayCluster
			Consistently(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(Equal(initialPendingClusterName), "Pending RayCluster name = %v", myRayService.Status.PendingServiceStatus.RayClusterName)

			// Verify that the pending RayCluster eventually reflects the changes in WorkerGroupSpecs
			Eventually(
				getPendingRayClusterWorkerGroupSpecsFunc(ctx, myRayService),
				time.Second*15,
				time.Millisecond*500,
			).Should(
				HaveLen(oldNumWorkerGroupSpecs + 1),
			)

			// The pending RayCluster will become the active RayCluster after:
			// (1) The pending RayCluster's head Pod becomes Running and Ready
			// (2) The pending RayCluster's Serve Deployments are HEALTHY.
			updateHeadPodToRunningAndReady(ctx, initialPendingClusterName, "default")
			healthyStatus := generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING)
			fakeRayDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{testServeAppName: &healthyStatus})
			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(BeEmpty(), "Pending RayCluster name = %v", myRayService.Status.PendingServiceStatus.RayClusterName)
			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Equal(initialPendingClusterName), "New active RayCluster name = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)
		})

		It("Status should be updated if the differences are not only LastUpdateTime and HealthLastUpdateTime fields.", func() {
			// Make sure (1) Dashboard client is healthy (2) All the three Ray Serve deployments in the active RayCluster are HEALTHY.
			initialClusterName, _ := getRayClusterNameFunc(ctx, myRayService)()
			Eventually(
				checkServiceHealth(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)

			// Change serve status to be unhealthy
			unhealthyStatus := generateServeStatus(rayv1.DeploymentStatusEnum.UNHEALTHY, rayv1.ApplicationStatusEnum.UNHEALTHY)
			fakeRayDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{testServeAppName: &unhealthyStatus})

			// Confirm not switch to a new RayCluster.
			Consistently(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(Equal(initialClusterName), "Active RayCluster name = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			// Check if all the deployment statuses are UNHEALTHY.
			checkAllDeploymentStatusesUnhealthy := func(ctx context.Context, rayService *rayv1.RayService) bool {
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayService.Name, Namespace: rayService.Namespace}, rayService); err != nil {
					return false
				}
				for _, appStatus := range rayService.Status.ActiveServiceStatus.Applications {
					for _, deploymentStatus := range appStatus.Deployments {
						if deploymentStatus.Status != rayv1.DeploymentStatusEnum.UNHEALTHY {
							return false
						}
					}
				}
				return true
			}

			// The status update not only includes the LastUpdateTime and HealthLastUpdateTime fields, but also the ServeStatuses[i].Status field.
			// Hence, all the ServeStatuses[i].Status should be updated to UNHEALTHY.
			//
			// Note: LastUpdateTime/HealthLastUpdateTime will be overwritten via metav1.Now() in rayservice_controller.go.
			// Hence, we cannot use `newTime` to check whether the status is updated or not.
			Eventually(
				checkAllDeploymentStatusesUnhealthy).WithContext(ctx).WithArguments(myRayService).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)

			healthyStatus := generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING)
			fakeRayDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{testServeAppName: &healthyStatus})

			// Confirm not switch to a new RayCluster.
			Consistently(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(Equal(initialClusterName), "Active RayCluster name = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			// The status update not only includes the LastUpdateTime and HealthLastUpdateTime fields, but also the ServeStatuses[i].Status field.
			// Hence, the status should be updated.
			Eventually(
				checkServiceHealth(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)
		})

		It("Status should not be updated if the only differences are the LastUpdateTime and HealthLastUpdateTime fields.", func() {
			// Make sure (1) Dashboard client is healthy (2) All the three Ray Serve deployments in the active RayCluster are HEALTHY.
			initialClusterName, _ := getRayClusterNameFunc(ctx, myRayService)()
			Eventually(
				checkServiceHealth(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)

			// Only update the LastUpdateTime and HealthLastUpdateTime fields in the active RayCluster.
			oldTime := myRayService.Status.ActiveServiceStatus.Applications[utils.DefaultServeAppName].HealthLastUpdateTime.DeepCopy()
			healthyStatus := generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING)
			fakeRayDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{testServeAppName: &healthyStatus})

			// Confirm not switch to a new RayCluster
			Consistently(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(Equal(initialClusterName), "Active RayCluster name = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			// The status is still the same as before.
			Eventually(
				checkServiceHealth(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)

			// Status should not be updated if the only differences are the LastUpdateTime and HealthLastUpdateTime fields.
			// Unlike the test "Status should be updated if the differences are not only LastUpdateTime and HealthLastUpdateTime fields.",
			// the status update will not be triggered, so we can check whether the LastUpdateTime/HealthLastUpdateTime fields are updated or not by `oldTime`.
			Expect(myRayService.Status.ActiveServiceStatus.Applications[utils.DefaultServeAppName].HealthLastUpdateTime).Should(Equal(oldTime), "myRayService status = %v", myRayService.Status)
		})

		It("Update workerGroup.replicas in RayService and should not switch to new Ray Cluster", func() {
			// Certain field updates should not trigger new RayCluster preparation, such as updates
			// to `Replicas` and `WorkersToDelete` triggered by the autoscaler during scaling up/down.
			// See the function `generateRayClusterJsonHash` for more details.
			initialClusterName, _ := getRayClusterNameFunc(ctx, myRayService)()
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
				*myRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas++
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource")

			// Confirm not switch to a new RayCluster
			Consistently(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(Equal(initialClusterName), "My current RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Status.ActiveServiceStatus.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
		})

		It("should perform a zero-downtime update after a code change.", func() {
			initialClusterName, _ := getRayClusterNameFunc(ctx, myRayService)()

			// The cluster shouldn't switch until deployments are finished updating
			updatingStatus := generateServeStatus(rayv1.DeploymentStatusEnum.UPDATING, rayv1.ApplicationStatusEnum.DEPLOYING)
			fakeRayDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{testServeAppName: &updatingStatus})
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)
				myRayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Env[1].Value = "UPDATED_VALUE"
				myRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Env[1].Value = "UPDATED_VALUE"
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource")

			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*60, time.Millisecond*500).Should(Not(BeEmpty()), "My new RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)

			pendingRayClusterName := myRayService.Status.PendingServiceStatus.RayClusterName

			Consistently(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(Equal(initialClusterName), "My current RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			// The cluster should switch once the deployments are finished updating
			healthyStatus := generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING)
			fakeRayDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{testServeAppName: &healthyStatus})
			updateHeadPodToRunningAndReady(ctx, pendingRayClusterName, "default")

			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*60, time.Millisecond*500).Should(Equal(pendingRayClusterName), "My current RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)
		})
	})
})
