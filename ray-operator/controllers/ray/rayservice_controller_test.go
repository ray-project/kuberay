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

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"

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

	var numReplicas int32
	var numCpus float64
	numReplicas = 1
	numCpus = 0.1

	myRayService := &rayiov1alpha1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayservice-sample",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayServiceSpec{
			ServeDeploymentGraphSpec: rayiov1alpha1.ServeDeploymentGraphSpec{
				ImportPath: "fruit.deployment_graph",
				RuntimeEnv: runtimeEnvStr,
				ServeConfigSpecs: []rayiov1alpha1.ServeConfigSpec{
					{
						Name:        "MangoStand",
						NumReplicas: &numReplicas,
						UserConfig:  "price: 3",
						RayActorOptions: rayiov1alpha1.RayActorOptionSpec{
							NumCpus: &numCpus,
						},
					},
					{
						Name:        "OrangeStand",
						NumReplicas: &numReplicas,
						UserConfig:  "price: 2",
						RayActorOptions: rayiov1alpha1.RayActorOptionSpec{
							NumCpus: &numCpus,
						},
					},
					{
						Name:        "PearStand",
						NumReplicas: &numReplicas,
						UserConfig:  "price: 1",
						RayActorOptions: rayiov1alpha1.RayActorOptionSpec{
							NumCpus: &numCpus,
						},
					},
				},
			},
			RayClusterSpec: rayiov1alpha1.RayClusterSpec{
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
		return &fakeRayDashboardClient
	}

	utils.GetRayHttpProxyClientFunc = utils.GetFakeRayHttpProxyClient

	myRayCluster := &rayiov1alpha1.RayCluster{}

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
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Status.ActiveServiceStatus.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
		})

		It("should create more than 1 worker", func() {
			filterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayService.Status.ActiveServiceStatus.RayClusterName, common.RayNodeGroupLabelKey: "small-group"}
			Eventually(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))
			if len(workerPods.Items) > 0 {
				Expect(workerPods.Items[0].Status.Phase).Should(Or(Equal(corev1.PodRunning), Equal(corev1.PodPending)))
			}
		})

		It("Dashboard should be healthy", func() {
			Eventually(
				checkServiceHealth(ctx, myRayService),
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "My myRayService status = %v", myRayService.Status)
		})

		It("should create a new head service resource", func() {
			svc := &corev1.Service{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: utils.GenerateServiceName(myRayService.Name), Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My head service = %v", svc)
			Expect(svc.Spec.Selector[common.RayIDLabelKey]).Should(Equal(utils.GenerateIdentifier(myRayCluster.Name, rayiov1alpha1.HeadNode)))
		})

		It("should create a new agent service resource", func() {
			svc := &corev1.Service{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: utils.GenerateDashboardServiceName(myRayCluster.Name), Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My agent service = %v", svc)
			Expect(svc.Spec.Selector[common.RayClusterDashboardServiceLabelKey]).Should(Equal(utils.GenerateDashboardAgentLabel(myRayCluster.Name)))
		})

		It("should create a new serve service resource", func() {
			svc := &corev1.Service{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: utils.GenerateServeServiceName(myRayService.Name), Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My serve service = %v", svc)
			Expect(svc.Spec.Selector[common.RayClusterLabelKey]).Should(Equal(myRayCluster.Name))
		})

		It("should update a rayservice object and switch to new Ray Cluster", func() {
			// adding a scale strategy
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Name, Namespace: "default"}, myRayService),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayService  = %v", myRayService.Name)

				podToDelete := workerPods.Items[0]
				myRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas = pointer.Int32(1)
				myRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{podToDelete.Name}

				return k8sClient.Update(ctx, myRayService)
			})

			Expect(err).NotTo(HaveOccurred(), "failed to update test RayService resource")

			// Confirm switch to a new Ray Cluster.
			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Not(Equal(myRayCluster.Name)), "My new RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)

			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayService.Status.ActiveServiceStatus.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
		})

		It("should detect unhealthy status and try to switch to new RayCluster.", func() {
			// Set deployment statuses to UNHEALTHY
			orignalServeDeploymentUnhealthySecondThreshold := ServiceUnhealthySecondThreshold
			ServiceUnhealthySecondThreshold = 5
			fakeRayDashboardClient.SetServeStatus(generateServeStatus(metav1.NewTime(time.Now().Add(time.Duration(-5)*time.Minute)), "UNHEALTHY"))

			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*60, time.Millisecond*500).Should(Not(BeEmpty()), "My new RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)

			ServiceUnhealthySecondThreshold = orignalServeDeploymentUnhealthySecondThreshold
			pendingRayClusterName := myRayService.Status.PendingServiceStatus.RayClusterName
			fakeRayDashboardClient.SetServeStatus(generateServeStatus(metav1.Now(), "HEALTHY"))

			Eventually(
				getPreparingRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(BeEmpty(), "My new RayCluster name  = %v", myRayService.Status.PendingServiceStatus.RayClusterName)
			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*15, time.Millisecond*500).Should(Equal(pendingRayClusterName), "My new RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)
		})

		It("should perform a zero-downtime update after a code change.", func() {
			initialClusterName, _ := getRayClusterNameFunc(ctx, myRayService)()

			// The cluster shouldn't switch until deployments are finished updating
			fakeRayDashboardClient.SetServeStatus(generateServeStatus(metav1.NewTime(time.Now().Add(time.Duration(-5)*time.Minute)), "UPDATING"))

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
			fakeRayDashboardClient.SetServeStatus(generateServeStatus(metav1.NewTime(time.Now().Add(time.Duration(-5)*time.Minute)), "HEALTHY"))

			Eventually(
				getRayClusterNameFunc(ctx, myRayService),
				time.Second*60, time.Millisecond*500).Should(Equal(pendingRayClusterName), "My current RayCluster name  = %v", myRayService.Status.ActiveServiceStatus.RayClusterName)
		})
	})
})

func prepareFakeRayDashboardClient() utils.FakeRayDashboardClient {
	client := utils.FakeRayDashboardClient{}

	client.SetServeStatus(generateServeStatus(metav1.Now(), "HEALTHY"))

	return client
}

func generateServeStatus(time metav1.Time, status string) utils.ServeDeploymentStatuses {
	serveStatuses := utils.ServeDeploymentStatuses{
		ApplicationStatus: rayiov1alpha1.AppStatus{
			Status:               "RUNNING",
			LastUpdateTime:       &time,
			HealthLastUpdateTime: &time,
		},
		DeploymentStatuses: []rayiov1alpha1.ServeDeploymentStatus{
			{
				Name:                 "shallow",
				Status:               status,
				Message:              "",
				LastUpdateTime:       &time,
				HealthLastUpdateTime: &time,
			},
			{
				Name:                 "deep",
				Status:               status,
				Message:              "",
				LastUpdateTime:       &time,
				HealthLastUpdateTime: &time,
			},
			{
				Name:                 "one",
				Status:               status,
				Message:              "",
				LastUpdateTime:       &time,
				HealthLastUpdateTime: &time,
			},
		},
	}

	return serveStatuses
}

func getRayClusterNameFunc(ctx context.Context, rayService *rayiov1alpha1.RayService) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayService.Name, Namespace: "default"}, rayService); err != nil {
			return "", err
		}
		return rayService.Status.ActiveServiceStatus.RayClusterName, nil
	}
}

func getPreparingRayClusterNameFunc(ctx context.Context, rayService *rayiov1alpha1.RayService) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayService.Name, Namespace: "default"}, rayService); err != nil {
			return "", err
		}
		return rayService.Status.PendingServiceStatus.RayClusterName, nil
	}
}

func checkServiceHealth(ctx context.Context, rayService *rayiov1alpha1.RayService) func() (bool, error) {
	return func() (bool, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayService.Name, Namespace: rayService.Namespace}, rayService); err != nil {
			return false, err
		}

		healthy := true

		healthy = healthy && rayService.Status.ActiveServiceStatus.DashboardStatus.IsHealthy
		healthy = healthy && (len(rayService.Status.ActiveServiceStatus.ServeStatuses) == 3)
		healthy = healthy && rayService.Status.ActiveServiceStatus.ServeStatuses[0].Status == "HEALTHY"
		healthy = healthy && rayService.Status.ActiveServiceStatus.ServeStatuses[1].Status == "HEALTHY"
		healthy = healthy && rayService.Status.ActiveServiceStatus.ServeStatuses[2].Status == "HEALTHY"

		return healthy, nil
	}
}
