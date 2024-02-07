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

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var myRayJob = &rayv1.RayJob{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rayjob-test",
		Namespace: "default",
	},
	Spec: rayv1.RayJobSpec{
		Entrypoint:               "sleep 999",
		ShutdownAfterJobFinishes: true,
		RayClusterSpec: &rayv1.RayClusterSpec{
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

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	myRayJob := myRayJob.DeepCopy()
	myRayJob.Name = "rayjob-test-default"

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
			myRayCluster := &rayv1.RayCluster{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
			Expect(myRayCluster.Labels).Should(HaveKeyWithValue(utils.RayOriginatedFromCRNameLabelKey, myRayJob.Name))
			Expect(myRayCluster.Labels).Should(HaveKeyWithValue(utils.RayOriginatedFromCRDLabelKey, utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD)))
		})

		It("Should create a number of workers equal to the replica setting", func() {
			filterLabels := client.MatchingLabels{utils.RayClusterLabelKey: myRayJob.Status.RayClusterName, utils.RayNodeGroupLabelKey: myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].GroupName}
			workerPods := corev1.PodList{}
			Eventually(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(int(*myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas)), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("should be able to update all Pods to Running", func() {
			myRayCluster := &rayv1.RayCluster{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
			Expect(myRayCluster.Status.State).NotTo(Equal(rayv1.Ready))

			// Update worker Pods to Running and PodReady.
			replicas := *myRayCluster.Spec.WorkerGroupSpecs[0].Replicas
			filterLabels := client.MatchingLabels{utils.RayClusterLabelKey: myRayJob.Status.RayClusterName, utils.RayNodeGroupLabelKey: myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].GroupName}
			workerPods := corev1.PodList{}
			Eventually(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(int(replicas)), fmt.Sprintf("workerGroup %v", workerPods.Items))
			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				// TODO: Check https://github.com/ray-project/kuberay/issues/1736.
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(BeNil())
			}

			// Update the head Pod to Running and PodReady.
			headPods := corev1.PodList{}
			headFilterLabels := client.MatchingLabels{utils.RayClusterLabelKey: myRayCluster.Name, utils.RayNodeGroupLabelKey: "headgroup"}
			err := k8sClient.List(ctx, &headPods, headFilterLabels, &client.ListOptions{Namespace: "default"}, client.InNamespace(myRayCluster.Namespace))
			Expect(err).NotTo(HaveOccurred(), "failed list head pods")
			Expect(len(headPods.Items)).Should(Equal(1), "My head pod list= %v", headPods.Items)
			headPod := headPods.Items[0]
			headPod.Status.Phase = corev1.PodRunning
			for _, cond := range headPod.Status.Conditions {
				if cond.Type == corev1.PodReady {
					cond.Status = corev1.ConditionTrue
				}
			}
			Expect(k8sClient.Status().Update(ctx, &headPod)).Should(BeNil())

			// The RayCluster.Status.State should be Ready.
			Eventually(
				getClusterState(ctx, "default", myRayCluster.Name),
				time.Second*15, time.Millisecond*500).Should(Equal(rayv1.Ready))
		})

		It("Dashboard URL should be set", func() {
			Eventually(
				getDashboardURLForRayJob(ctx, myRayJob),
				time.Second*1, time.Millisecond*500).Should(HavePrefix(myRayJob.Name), "Dashboard URL = %v", myRayJob.Status.DashboardURL)
		})

		It("test cluster selector", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayJob.Status.RayClusterName)
			myRayJobWithClusterSelector := &rayv1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rayjob-test-default-2",
					Namespace: "default",
				},
				Spec: rayv1.RayJobSpec{
					Entrypoint:      "sleep 999",
					ClusterSelector: map[string]string{},
				},
			}
			myRayJobWithClusterSelector.Spec.ClusterSelector[RayJobDefaultClusterSelectorKey] = myRayJob.Status.RayClusterName

			err := k8sClient.Create(ctx, myRayJobWithClusterSelector)
			Expect(err).NotTo(HaveOccurred(), "failed to create RayJob resource")

			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJobWithClusterSelector),
				time.Second*15, time.Millisecond*500).Should(Equal(myRayJob.Status.RayClusterName))
		})

		It("job reached terminal state, deployment status should be Completed with EndTime set", func() {
			now := time.Now()
			// update fake dashboard client to return job info with "Succeeded status"
			getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) {
				return &utils.RayJobInfo{JobStatus: rayv1.JobStatusSucceeded}, nil
			}
			fakeRayDashboardClient.GetJobInfoMock.Store(&getJobInfo)
			defer fakeRayDashboardClient.GetJobInfoMock.Store(nil)

			Eventually(
				getRayJobDeploymentStatus(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusComplete), "jobDeploymentStatus = %v", myRayJob.Status.JobDeploymentStatus)
			Expect(myRayJob.Status.EndTime.After(now)).Should(BeTrue(), "EndTime = %v, Now = %v", myRayJob.Status.EndTime, now)
		})

		It("job completed with ShutdownAfterJobFinishes=true, RayCluster should be deleted but not the submitter Job", func() {
			myRayCluster := &rayv1.RayCluster{}
			Eventually(
				func() bool {
					return apierrors.IsNotFound(getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster)())
				},
				time.Second*15, time.Millisecond*500).Should(BeTrue(), "My myRayJob  = %v", myRayJob.Name)

			submitterJob := &batchv1.Job{}
			Eventually(
				func() error {
					return getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Name, Namespace: "default"}, submitterJob)()
				},
				time.Second*5, time.Millisecond*500).Should(BeNil(), "Expected Kubernetes job to be present")
		})
	})
})

func getRayClusterNameForRayJob(ctx context.Context, rayJob *rayv1.RayJob) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.RayClusterName, nil
	}
}

func getDashboardURLForRayJob(ctx context.Context, rayJob *rayv1.RayJob) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.DashboardURL, nil
	}
}
