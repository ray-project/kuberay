package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobWithClusterSelector(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Job scripts
	jobs := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "jobs",
			Namespace: namespace.Name,
		},
		BinaryData: map[string][]byte{
			"counter.py": ReadFile(test, "counter.py"),
			"fail.py":    ReadFile(test, "fail.py"),
		},
		Immutable: Ptr(true),
	}
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Create(test.Ctx(), jobs, metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	// RayCluster
	rayCluster := &rayv1.RayCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rayv1.GroupVersion.String(),
			Kind:       "RayCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster",
			Namespace: namespace.Name,
		},
		Spec: rayv1.RayClusterSpec{
			RayVersion: GetRayVersion(),
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{
					"dashboard-host": "0.0.0.0",
				},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: GetRayImage(),
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: 6379,
										Name:          "gcs",
									},
									{
										ContainerPort: 8265,
										Name:          "dashboard",
									},
									{
										ContainerPort: 10001,
										Name:          "client",
									},
								},
								Lifecycle: &corev1.Lifecycle{
									PreStop: &corev1.LifecycleHandler{
										Exec: &corev1.ExecAction{
											Command: []string{"/bin/sh", "-c", "ray stop"},
										},
									},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("300m"),
										corev1.ResourceMemory: resource.MustParse("1G"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("500m"),
										corev1.ResourceMemory: resource.MustParse("2G"),
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "jobs",
										MountPath: "/home/ray/jobs",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "jobs",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: jobs.Name,
										},
									},
								},
							},
						},
					},
				},
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					Replicas:       Ptr(int32(1)),
					MinReplicas:    Ptr(int32(1)),
					MaxReplicas:    Ptr(int32(1)),
					GroupName:      "small-group",
					RayStartParams: map[string]string{},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-worker",
									Image: GetRayImage(),
									Lifecycle: &corev1.Lifecycle{
										PreStop: &corev1.LifecycleHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/bin/sh", "-c", "ray stop"},
											},
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("300m"),
											corev1.ResourceMemory: resource.MustParse("1G"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("500m"),
											corev1.ResourceMemory: resource.MustParse("1G"),
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
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Create(test.Ctx(), rayCluster, metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	test.T().Run("Successful RayJob", func(t *testing.T) {
		t.Parallel()

		// RayJob
		rayJob := &rayv1.RayJob{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rayv1.GroupVersion.String(),
				Kind:       "RayJob",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "counter",
				Namespace: namespace.Name,
			},
			Spec: rayv1.RayJobSpec{
				ClusterSelector: map[string]string{
					common.RayClusterLabelKey: rayCluster.Name,
				},
				Entrypoint: "python /home/ray/jobs/counter.py",
				RuntimeEnvYAML: `
env_vars:
  counter_name: test_counter
`,
				ShutdownAfterJobFinishes: false,
				SubmitterPodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-job-submitter",
								Image: GetRayImage(),
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("200m"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("500m"),
										corev1.ResourceMemory: resource.MustParse("500Mi"),
									},
								},
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		}
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Create(test.Ctx(), rayJob, metav1.CreateOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the Ray job has completed successfully
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)))
	})

	test.T().Run("Failing RayJob", func(t *testing.T) {
		t.Parallel()

		// RayJob
		rayJob := &rayv1.RayJob{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rayv1.GroupVersion.String(),
				Kind:       "RayJob",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fail",
				Namespace: namespace.Name,
			},
			Spec: rayv1.RayJobSpec{
				ClusterSelector: map[string]string{
					common.RayClusterLabelKey: rayCluster.Name,
				},
				Entrypoint:               "python /home/ray/jobs/fail.py",
				ShutdownAfterJobFinishes: false,
				SubmitterPodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-job-submitter",
								Image: GetRayImage(),
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("200m"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("500m"),
										corev1.ResourceMemory: resource.MustParse("500Mi"),
									},
								},
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		}
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Create(test.Ctx(), rayJob, metav1.CreateOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the Ray job has failed
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusFailed)))
	})
}
