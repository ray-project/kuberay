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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var runtimeEnvStr = "working_dir:\n - \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\""

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	var workerPods corev1.PodList

	var numReplicas int32 = 1
	var numCpus float64 = 0.1

	testServeAppName := "app1"
	testServeConfigV2 := fmt.Sprintf(`
applications:
  - name: %s
    import_path: fruit.deployment_graph
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
          num_cpus: 0.1
      - name: FruitMarket
        num_replicas: 1
        ray_actor_options:
          num_cpus: 0.1
      - name: DAGDriver
        num_replicas: 1
        route_prefix: "/"
        ray_actor_options:
          num_cpus: 0.1`, testServeAppName)

	myRayService := &rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayservice-sample",
			Namespace: "default",
		},
		Spec: rayv1.RayServiceSpec{
			ServeDeploymentGraphSpec: rayv1.ServeDeploymentGraphSpec{
				ImportPath: "fruit.deployment_graph",
				RuntimeEnv: runtimeEnvStr,
				ServeConfigSpecs: []rayv1.ServeConfigSpec{
					{
						Name:        "MangoStand",
						NumReplicas: &numReplicas,
						UserConfig:  "price: 3",
						RayActorOptions: rayv1.RayActorOptionSpec{
							NumCpus: &numCpus,
						},
					},
					{
						Name:        "OrangeStand",
						NumReplicas: &numReplicas,
						UserConfig:  "price: 2",
						RayActorOptions: rayv1.RayActorOptionSpec{
							NumCpus: &numCpus,
						},
					},
					{
						Name:        "PearStand",
						NumReplicas: &numReplicas,
						UserConfig:  "price: 1",
						RayActorOptions: rayv1.RayActorOptionSpec{
							NumCpus: &numCpus,
						},
					},
				},
			},
			RayClusterSpec: rayv1.RayClusterSpec{
				RayVersion: "1.12.1",
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
								"groupName": "headgroup",
							},
							Annotations: map[string]string{
								"key": "value",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: "rayproject/ray:2.7.0",
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
									"groupName": "small-group",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:    "ray-worker",
										Image:   "rayproject/ray:2.7.0",
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

	fakeRayDashboardClient := prepareFakeRayDashboardClient()

	utils.GetRayDashboardClientFunc = func() utils.RayDashboardClientInterface {
		return fakeRayDashboardClient
	}

	utils.GetRayHttpProxyClientFunc = utils.GetFakeRayHttpProxyClient

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
			updateHeadPodToRunningAndReady(ctx, pendingRayClusterName)

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
			filterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayService.Status.ActiveServiceStatus.RayClusterName, common.RayNodeGroupLabelKey: "small-group"}
			Eventually(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))
			if len(workerPods.Items) > 0 {
				Expect(workerPods.Items[0].Status.Phase).Should(Or(Equal(corev1.PodRunning), Equal(corev1.PodPending)))
				// All the worker Pods should have a port with the name "dashboard-agent"
				for _, pod := range workerPods.Items {
					// Worker Pod should have only one container.
					Expect(len(pod.Spec.Containers)).Should(Equal(1))
					// Each worker Pod should have a container port with the name "dashboard-agent"
					exist := false
					for _, port := range pod.Spec.Containers[0].Ports {
						if port.Name == common.DashboardAgentListenPortName {
							exist = true
							break
						}
					}
					if !exist {
						Fail(fmt.Sprintf("Worker Pod %v should have a container port with the name %v", pod.Name, common.DashboardAgentListenPortName))
					}
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
			Expect(err).To(BeNil(), "failed to generate head service name")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: headSvcName, Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My head service = %v", svc)
			Expect(svc.Spec.Selector[common.RayIDLabelKey]).Should(Equal(utils.GenerateIdentifier(myRayCluster.Name, rayv1.HeadNode)))
		})

		It("should create a new serve service resource", func() {
			svc := &corev1.Service{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: utils.GenerateServeServiceName(myRayService.Name), Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My serve service = %v", svc)
			Expect(svc.Spec.Selector[common.RayClusterLabelKey]).Should(Equal(myRayCluster.Name))
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
			updateHeadPodToRunningAndReady(ctx, pendingRayClusterName)

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
			updateHeadPodToRunningAndReady(ctx, initialPendingClusterName)
			fakeRayDashboardClient.SetSingleApplicationStatus(generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING))
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

			// ServiceUnhealthySecondThreshold is a global variable in rayservice_controller.go.
			// If the time elapsed since the last update of the service HEALTHY status exceeds ServiceUnhealthySecondThreshold seconds,
			// the RayService controller will consider the active RayCluster as unhealthy and prepare a new RayCluster.
			originalServiceUnhealthySecondThreshold := ServiceUnhealthySecondThreshold
			ServiceUnhealthySecondThreshold = 500

			// Change serve status to be unhealthy
			fakeRayDashboardClient.SetSingleApplicationStatus(generateServeStatus(rayv1.DeploymentStatusEnum.UNHEALTHY, rayv1.ApplicationStatusEnum.UNHEALTHY))

			// Confirm not switch to a new RayCluster because ServiceUnhealthySecondThreshold is 500 seconds.
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
				checkAllDeploymentStatusesUnhealthy(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)

			fakeRayDashboardClient.SetSingleApplicationStatus(generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING))

			// Confirm not switch to a new RayCluster because ServiceUnhealthySecondThreshold is 500 seconds.
			Consistently(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(Equal(initialClusterName), "Active RayCluster name = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			// The status update not only includes the LastUpdateTime and HealthLastUpdateTime fields, but also the ServeStatuses[i].Status field.
			// Hence, the status should be updated.
			Eventually(
				checkServiceHealth(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)
			ServiceUnhealthySecondThreshold = originalServiceUnhealthySecondThreshold
		})

		It("Status should not be updated if the only differences are the LastUpdateTime and HealthLastUpdateTime fields.", func() {
			// Make sure (1) Dashboard client is healthy (2) All the three Ray Serve deployments in the active RayCluster are HEALTHY.
			initialClusterName, _ := getRayClusterNameFunc(ctx, myRayService)()
			Eventually(
				checkServiceHealth(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)

			// Only update the LastUpdateTime and HealthLastUpdateTime fields in the active RayCluster.
			oldTime := myRayService.Status.ActiveServiceStatus.Applications[common.DefaultServeAppName].HealthLastUpdateTime.DeepCopy()
			fakeRayDashboardClient.SetSingleApplicationStatus(generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING))

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
			Expect(myRayService.Status.ActiveServiceStatus.Applications[common.DefaultServeAppName].HealthLastUpdateTime).Should(Equal(oldTime), "myRayService status = %v", myRayService.Status)
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
			fakeRayDashboardClient.SetSingleApplicationStatus(generateServeStatus(rayv1.DeploymentStatusEnum.UPDATING, rayv1.ApplicationStatusEnum.DEPLOYING))

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
			fakeRayDashboardClient.SetSingleApplicationStatus(generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING))
			updateHeadPodToRunningAndReady(ctx, pendingRayClusterName)

			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*60, time.Millisecond*500).Should(Equal(pendingRayClusterName), "My current RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)
		})

		It("should reconcile status correctly when multi-app config type is used.", func() {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)

				myRayService.Spec.ServeDeploymentGraphSpec = rayv1.ServeDeploymentGraphSpec{}
				myRayService.Spec.ServeConfigV2 = testServeConfigV2
				return k8sClient.Update(ctx, myRayService)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService serve config")

			// Set multi-application status to healthy.
			healthyStatus := generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING)
			fakeRayDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{testServeAppName: &healthyStatus})
			// Set single-application status to unhealthy.
			unhealthyStatus := generateServeStatus(rayv1.DeploymentStatusEnum.UNHEALTHY, rayv1.ApplicationStatusEnum.UNHEALTHY)
			fakeRayDashboardClient.SetSingleApplicationStatus(unhealthyStatus)

			// The status should remain healthy, because we have set serveConfigTypeForTesting to MULTI_APP
			// Then the rayservice controller should pull the deployment statuses from the multi-app API and not the single-app API
			// So regardless of what the single-app API is returning, the status of the rayservice should remain healthy
			// because the multi-app API is returning healthy statuses.
			Consistently(
				checkServiceHealth(ctx, myRayService),
				time.Second*5, time.Millisecond*500).Should(BeTrue(), "myRayService status = %v", myRayService.Status)

			// reset states to healthy for following test cases
			fakeRayDashboardClient.SetSingleApplicationStatus(healthyStatus)
		})
	})
})

func prepareFakeRayDashboardClient() *utils.FakeRayDashboardClient {
	client := &utils.FakeRayDashboardClient{}

	client.SetSingleApplicationStatus(generateServeStatus(rayv1.DeploymentStatusEnum.HEALTHY, rayv1.ApplicationStatusEnum.RUNNING))

	return client
}

func generateServeStatus(deploymentStatus string, applicationStatus string) utils.ServeApplicationStatus {
	return utils.ServeApplicationStatus{
		Status: applicationStatus,
		Deployments: map[string]utils.ServeDeploymentStatus{
			"shallow": {
				Name:    "shallow",
				Status:  deploymentStatus,
				Message: "",
			},
			"deep": {
				Name:    "deep",
				Status:  deploymentStatus,
				Message: "",
			},
			"one": {
				Name:    "one",
				Status:  deploymentStatus,
				Message: "",
			},
		},
	}
}

func getRayClusterNameFunc(ctx context.Context, rayService *rayv1.RayService) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayService.Name, Namespace: "default"}, rayService); err != nil {
			return "", err
		}
		return rayService.Status.ActiveServiceStatus.RayClusterName, nil
	}
}

func getPreparingRayClusterNameFunc(ctx context.Context, rayService *rayv1.RayService) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayService.Name, Namespace: "default"}, rayService); err != nil {
			return "", err
		}
		return rayService.Status.PendingServiceStatus.RayClusterName, nil
	}
}

func checkServiceHealth(ctx context.Context, rayService *rayv1.RayService) func() (bool, error) {
	return func() (bool, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayService.Name, Namespace: rayService.Namespace}, rayService); err != nil {
			return false, err
		}

		if !rayService.Status.ActiveServiceStatus.DashboardStatus.IsHealthy {
			return false, nil
		}

		for _, appStatus := range rayService.Status.ActiveServiceStatus.Applications {
			if appStatus.Status != rayv1.ApplicationStatusEnum.RUNNING {
				return false, nil
			}
			for _, deploymentStatus := range appStatus.Deployments {
				if deploymentStatus.Status != rayv1.DeploymentStatusEnum.HEALTHY {
					return false, nil
				}
			}
		}

		return true, nil
	}
}

// Update the status of the head Pod to Running.
// We need to manually update Pod statuses otherwise they'll always be Pending.
// envtest doesn't create a full K8s cluster. It's only the control plane.
// There's no container runtime or any other K8s controllers.
// So Pods are created, but no controller updates them from Pending to Running.
// See https://book.kubebuilder.io/reference/envtest.html for more details.
func updateHeadPodToRunningAndReady(ctx context.Context, rayClusterName string) {
	headPods := corev1.PodList{}
	headFilterLabels := client.MatchingLabels{
		common.RayClusterLabelKey:  rayClusterName,
		common.RayNodeTypeLabelKey: string(rayv1.HeadNode),
	}

	Eventually(
		listResourceFunc(ctx, &headPods, headFilterLabels, &client.ListOptions{Namespace: "default"}),
		time.Second*15, time.Millisecond*500).Should(Equal(1), "Head pod list should have only 1 Pod = %v", headPods.Items)

	headPod := headPods.Items[0]
	headPod.Status.Phase = corev1.PodRunning
	headPod.Status.Conditions = []corev1.PodCondition{
		{
			Type:   corev1.PodReady,
			Status: corev1.ConditionTrue,
		},
	}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return k8sClient.Status().Update(ctx, &headPod)
	})
	Expect(err).NotTo(HaveOccurred(), "Failed to update head Pod status to PodRunning")

	// Make sure the head Pod is updated.
	Eventually(
		isAllPodsRunning(ctx, headPods, headFilterLabels, "default"),
		time.Second*15, time.Millisecond*500).Should(BeTrue(), "Head Pod should be running: %v", headPods.Items)
}
