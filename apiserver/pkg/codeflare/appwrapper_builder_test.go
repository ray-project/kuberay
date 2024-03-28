package codeflare_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	mcadApi "github.com/project-codeflare/multi-cluster-app-dispatcher/pkg/apis/controller/v1beta1"
	diff "github.com/r3labs/diff/v3"
	"github.com/ray-project/kuberay/apiserver/pkg/codeflare"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func Ptr[T any](v T) *T {
	return &v
}

func TestRayClusterToAppWrapperConversion(t *testing.T) {
	appwrapperBuilder := codeflare.NewAppWrapperConverter(setupScheme(t))

	tests := []struct {
		Name               string
		inputCluster       *rayv1api.RayCluster
		expectedAppwrapper *mcadApi.AppWrapper
		expectedError      error
	}{
		{
			Name: "CPU only cluster",
			inputCluster: &rayv1api.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cpu-only-cluster",
					Namespace: "cpu-only-cluster",
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "RayCluster",
					APIVersion: "ray.io/v1",
				},
				Spec: rayv1api.RayClusterSpec{
					HeadGroupSpec: rayv1api.HeadGroupSpec{
						Template: v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Limits: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("1000m"),
												"memory": resource.MustParse("128Mi"),
											},
											Requests: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("500m"),
												"memory": resource.MustParse("64Mi"),
											},
										},
									},
								},
								InitContainers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Limits: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("1000m"),
												"memory": resource.MustParse("128Mi"),
											},
											Requests: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("500m"),
												"memory": resource.MustParse("64Mi"),
											},
										},
									},
								},
							},
						},
					},
					WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{
						{
							GroupName:      "group-1",
							Replicas:       Ptr(int32(1)),
							MinReplicas:    Ptr(int32(1)),
							MaxReplicas:    Ptr(int32(1)),
							RayStartParams: map[string]string{},
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									InitContainers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Limits: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("1000m"),
													"memory": resource.MustParse("128Mi"),
												},
												Requests: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("500m"),
													"memory": resource.MustParse("64Mi"),
												},
											},
										},
									},
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Limits: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("1000m"),
													"memory": resource.MustParse("128Mi"),
												},
												Requests: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("500m"),
													"memory": resource.MustParse("64Mi"),
												},
											},
										},
									},
								},
							},
						},
					},
					RayVersion: "2.9.0",
				},
			},
			expectedAppwrapper: &mcadApi.AppWrapper{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cpu-only-cluster",
					Namespace: "cpu-only-cluster",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: mcadApi.AppWrapperSpec{
					AggrResources: mcadApi.AppWrapperResourceList{
						GenericItems: []mcadApi.AppWrapperGenericResource{
							{
								CustomPodResources: []mcadApi.CustomPodResourceTemplate{
									{
										Replicas: 1,
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("2000m"),
											"memory": resource.MustParse("256Mi"),
										},
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("4000m"),
											"memory": resource.MustParse("512Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			Name: "Cluster with worker GPU",
			inputCluster: &rayv1api.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-with-GPU",
					Namespace: "cluster-with-GPU",
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "RayCluster",
					APIVersion: "ray.io/v1",
				},
				Spec: rayv1api.RayClusterSpec{
					HeadGroupSpec: rayv1api.HeadGroupSpec{
						Template: v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Limits: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("1000m"),
												"memory": resource.MustParse("128Mi"),
											},
											Requests: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("500m"),
												"memory": resource.MustParse("64Mi"),
											},
										},
									},
								},
								InitContainers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Limits: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("1000m"),
												"memory": resource.MustParse("128Mi"),
											},
											Requests: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("500m"),
												"memory": resource.MustParse("64Mi"),
											},
										},
									},
								},
							},
						},
					},
					WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{
						{
							GroupName:      "group-1",
							Replicas:       Ptr(int32(1)),
							MinReplicas:    Ptr(int32(1)),
							MaxReplicas:    Ptr(int32(1)),
							RayStartParams: map[string]string{},
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									InitContainers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Limits: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("1000m"),
													"memory": resource.MustParse("128Mi"),
												},
												Requests: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("500m"),
													"memory": resource.MustParse("64Mi"),
												},
											},
										},
									},
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Limits: map[v1.ResourceName]resource.Quantity{
													"cpu":            resource.MustParse("1000m"),
													"memory":         resource.MustParse("128Mi"),
													"nvidia.com/gpu": resource.MustParse("1.0"),
												},
												Requests: map[v1.ResourceName]resource.Quantity{
													"cpu":            resource.MustParse("500m"),
													"memory":         resource.MustParse("64Mi"),
													"nvidia.com/gpu": resource.MustParse("1.0"),
												},
											},
										},
									},
								},
							},
						},
					},
					RayVersion: "2.9.0",
				},
			},
			expectedAppwrapper: &mcadApi.AppWrapper{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-with-GPU",
					Namespace: "cluster-with-GPU",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: mcadApi.AppWrapperSpec{
					AggrResources: mcadApi.AppWrapperResourceList{
						GenericItems: []mcadApi.AppWrapperGenericResource{
							{
								CustomPodResources: []mcadApi.CustomPodResourceTemplate{
									{
										Replicas: 1,
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":            resource.MustParse("2000m"),
											"memory":         resource.MustParse("256Mi"),
											"nvidia.com/gpu": resource.MustParse("1.0"),
										},
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":            resource.MustParse("4000m"),
											"memory":         resource.MustParse("512Mi"),
											"nvidia.com/gpu": resource.MustParse("1.0"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			Name: "Variable size CPU cluster",
			inputCluster: &rayv1api.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "variable-pod-cluster",
					Namespace: "variable-pod-cluster",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: rayv1api.RayClusterSpec{
					HeadGroupSpec: rayv1api.HeadGroupSpec{
						Template: v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Limits: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("1000m"),
												"memory": resource.MustParse("128Mi"),
											},
											Requests: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("500m"),
												"memory": resource.MustParse("64Mi"),
											},
										},
									},
								},
							},
						},
					},
					WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{
						{
							GroupName:      "group-1",
							Replicas:       Ptr(int32(2)),
							MinReplicas:    Ptr(int32(1)),
							MaxReplicas:    Ptr(int32(2)),
							RayStartParams: map[string]string{},
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Limits: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("1000m"),
													"memory": resource.MustParse("128Mi"),
												},
												Requests: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("500m"),
													"memory": resource.MustParse("64Mi"),
												},
											},
										},
									},
								},
							},
						},
					},
					RayVersion: "2.9.0",
				},
			},
			expectedAppwrapper: &mcadApi.AppWrapper{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "variable-pod-cluster",
					Namespace: "variable-pod-cluster",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: mcadApi.AppWrapperSpec{
					AggrResources: mcadApi.AppWrapperResourceList{
						GenericItems: []mcadApi.AppWrapperGenericResource{
							{
								CustomPodResources: []mcadApi.CustomPodResourceTemplate{
									{
										Replicas: 1,
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("1500m"),
											"memory": resource.MustParse("192Mi"),
										},
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("2500m"),
											"memory": resource.MustParse("384Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			Name: "CPU only cluster with no init containers",
			inputCluster: &rayv1api.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cpu-only-cluster-no-init-containers",
					Namespace: "cpu-only-cluster-no-init-containers",
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "RayCluster",
					APIVersion: "ray.io/v1",
				},
				Spec: rayv1api.RayClusterSpec{
					HeadGroupSpec: rayv1api.HeadGroupSpec{
						Template: v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Limits: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("1000m"),
												"memory": resource.MustParse("128Mi"),
											},
											Requests: map[v1.ResourceName]resource.Quantity{
												"cpu":    resource.MustParse("500m"),
												"memory": resource.MustParse("64Mi"),
											},
										},
									},
								},
							},
						},
					},
					WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{
						{
							GroupName:      "group-1",
							Replicas:       Ptr(int32(1)),
							MinReplicas:    Ptr(int32(1)),
							MaxReplicas:    Ptr(int32(1)),
							RayStartParams: map[string]string{},
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Limits: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("1000m"),
													"memory": resource.MustParse("128Mi"),
												},
												Requests: map[v1.ResourceName]resource.Quantity{
													"cpu":    resource.MustParse("500m"),
													"memory": resource.MustParse("64Mi"),
												},
											},
										},
									},
								},
							},
						},
					},
					RayVersion: "2.9.0",
				},
			},
			expectedAppwrapper: &mcadApi.AppWrapper{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cpu-only-cluster-no-init-containers",
					Namespace: "cpu-only-cluster-no-init-containers",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: mcadApi.AppWrapperSpec{
					AggrResources: mcadApi.AppWrapperResourceList{
						GenericItems: []mcadApi.AppWrapperGenericResource{
							{
								CustomPodResources: []mcadApi.CustomPodResourceTemplate{
									{
										Replicas: 1,
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("1000m"),
											"memory": resource.MustParse("128Mi"),
										},
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("2000m"),
											"memory": resource.MustParse("256Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
	}
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualAppwrapper, err := appwrapperBuilder.AppWrapperFromRayCluster(tc.inputCluster)
			if tc.expectedError == nil {
				require.NotNil(t, actualAppwrapper, "An actual appwrapper is expected")
				compareAppWrappers(t, tc.expectedAppwrapper, actualAppwrapper)
			} else {
				require.EqualError(t, err, tc.expectedError.Error(), "Matching error expected")
			}
		})
	}
}

func TestAppWrapperToRayClusterConversion(t *testing.T) {
	appwrapperConverter := codeflare.NewAppWrapperConverter(setupScheme(t))

	inputCluster := &rayv1api.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cpu-only-cluster",
			Namespace: "cpu-only-cluster",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "RayCluster",
			APIVersion: "ray.io/v1",
		},
		Spec: rayv1api.RayClusterSpec{
			HeadGroupSpec: rayv1api.HeadGroupSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: map[v1.ResourceName]resource.Quantity{
										"cpu":    resource.MustParse("1000m"),
										"memory": resource.MustParse("128Mi"),
									},
									Requests: map[v1.ResourceName]resource.Quantity{
										"cpu":    resource.MustParse("500m"),
										"memory": resource.MustParse("64Mi"),
									},
								},
							},
						},
						InitContainers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: map[v1.ResourceName]resource.Quantity{
										"cpu":    resource.MustParse("1000m"),
										"memory": resource.MustParse("128Mi"),
									},
									Requests: map[v1.ResourceName]resource.Quantity{
										"cpu":    resource.MustParse("500m"),
										"memory": resource.MustParse("64Mi"),
									},
								},
							},
						},
					},
				},
			},
			WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{
				{
					GroupName:      "group-1",
					Replicas:       Ptr(int32(1)),
					MinReplicas:    Ptr(int32(1)),
					MaxReplicas:    Ptr(int32(1)),
					RayStartParams: map[string]string{},
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							InitContainers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("1000m"),
											"memory": resource.MustParse("128Mi"),
										},
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("500m"),
											"memory": resource.MustParse("64Mi"),
										},
									},
								},
							},
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("1000m"),
											"memory": resource.MustParse("128Mi"),
										},
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("500m"),
											"memory": resource.MustParse("64Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			RayVersion: "2.9.0",
		},
	}
	clusterAppWrapper, err := appwrapperConverter.AppWrapperFromRayCluster(inputCluster)
	require.NoError(t, err, "No error expected when converting to appwrapper")
	require.NotNil(t, inputCluster, "A cluster app wrapper is expected")
	rayCluster, err := appwrapperConverter.RayClusterFromAppWrapper(clusterAppWrapper)
	require.NoError(t, err, "No error expe ted when converting from appwrapper")
	require.NotNil(t, rayCluster, "A cluster object is expected")
	diffs, err := diff.Diff(inputCluster, rayCluster,
		diff.SliceOrdering(true),
		diff.ConvertCompatibleTypes(),
		diff.StructMapKeySupport(),
		diff.CustomValueDiffers(&QuantityDiffer{}))
	if assert.NoError(t, err) && len(diffs) > 0 {
		t.Logf("No differences expected in cluster specs expected, yet there are some...")
		dumpChangeLog(t, diffs)
		t.Fail()
	}
}

func TestAppWrapperToRayJobConversion(t *testing.T) {
	appwrapperConverter := codeflare.NewAppWrapperConverter(setupScheme(t))
	inputRayJob := &rayv1api.RayJob{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RayJob",
			APIVersion: "ray.io/v1",
		},
	}
	rayJobAppWrapper, err := appwrapperConverter.AppWrapperFromRayJob(inputRayJob)
	require.NoError(t, err, "No error expected converting a ray job to an appwrapper")
	require.NotNil(t, rayJobAppWrapper, "An appwrapper is required from conversion")
	rayJob, err := appwrapperConverter.RayJobFromAppWrapper(rayJobAppWrapper)
	require.NoError(t, err, "No error expected converting an appwrapper to a ray job")
	require.NotNil(t, rayJob, "A ray job is expected when converting from appwrapper")
	diffs, err := diff.Diff(inputRayJob, rayJob,
		diff.SliceOrdering(true),
		diff.ConvertCompatibleTypes(),
		diff.StructMapKeySupport(),
		diff.CustomValueDiffers(&QuantityDiffer{}))
	if assert.NoError(t, err) && len(diffs) > 0 {
		t.Logf("No differences expected in cluster specs expected, yet there are some...")
		dumpChangeLog(t, diffs)
		t.Fail()
	}
}

func TestRayJobToAppWrapperConversion(t *testing.T) {
	appwrapperBuilder := codeflare.NewAppWrapperConverter(setupScheme(t))

	testClusterSpec := &rayv1api.RayClusterSpec{
		HeadGroupSpec: rayv1api.HeadGroupSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"cpu":    resource.MustParse("1000m"),
									"memory": resource.MustParse("128Mi"),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"cpu":    resource.MustParse("500m"),
									"memory": resource.MustParse("64Mi"),
								},
							},
						},
					},
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"cpu":    resource.MustParse("1000m"),
									"memory": resource.MustParse("128Mi"),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"cpu":    resource.MustParse("500m"),
									"memory": resource.MustParse("64Mi"),
								},
							},
						},
					},
				},
			},
		},
		WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{
			{
				GroupName:      "group-1",
				Replicas:       Ptr(int32(1)),
				MinReplicas:    Ptr(int32(1)),
				MaxReplicas:    Ptr(int32(1)),
				RayStartParams: map[string]string{},
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						InitContainers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: map[v1.ResourceName]resource.Quantity{
										"cpu":    resource.MustParse("1000m"),
										"memory": resource.MustParse("128Mi"),
									},
									Requests: map[v1.ResourceName]resource.Quantity{
										"cpu":    resource.MustParse("500m"),
										"memory": resource.MustParse("64Mi"),
									},
								},
							},
						},
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: map[v1.ResourceName]resource.Quantity{
										"cpu":    resource.MustParse("1000m"),
										"memory": resource.MustParse("128Mi"),
									},
									Requests: map[v1.ResourceName]resource.Quantity{
										"cpu":    resource.MustParse("500m"),
										"memory": resource.MustParse("64Mi"),
									},
								},
							},
						},
					},
				},
			},
		},
		RayVersion: "2.9.0",
	}

	tests := []struct {
		Name               string
		inputJob           *rayv1api.RayJob
		expectedAppwrapper *mcadApi.AppWrapper
		expectedError      error
	}{
		{
			Name: "A job with a cluster and a submitter spec",
			inputJob: &rayv1api.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "a-job-with-cluster",
					Namespace: "a-job-with-cluster",
				},
				Spec: rayv1api.RayJobSpec{
					Entrypoint:               "",
					Metadata:                 map[string]string{},
					RuntimeEnvYAML:           "",
					ShutdownAfterJobFinishes: false,
					TTLSecondsAfterFinished:  0,
					RayClusterSpec:           testClusterSpec,
					SubmitterPodTemplate: &v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("100m"),
											"memory": resource.MustParse("16Mi"),
										},
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("100m"),
											"memory": resource.MustParse("16Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedAppwrapper: &mcadApi.AppWrapper{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "a-job-with-cluster",
					Namespace: "a-job-with-cluster",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: mcadApi.AppWrapperSpec{
					AggrResources: mcadApi.AppWrapperResourceList{
						GenericItems: []mcadApi.AppWrapperGenericResource{
							{
								CustomPodResources: []mcadApi.CustomPodResourceTemplate{
									{
										Replicas: 1,
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("2100m"),
											"memory": resource.MustParse("272Mi"),
										},
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("4100m"),
											"memory": resource.MustParse("528Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			Name: "A job with a cluster and no submitter spec",
			inputJob: &rayv1api.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "a-job-with-cluster",
					Namespace: "a-job-with-cluster",
				},
				Spec: rayv1api.RayJobSpec{
					Entrypoint:               "",
					Metadata:                 map[string]string{},
					RuntimeEnvYAML:           "",
					ShutdownAfterJobFinishes: false,
					TTLSecondsAfterFinished:  0,
					RayClusterSpec:           testClusterSpec,
				},
			},
			expectedAppwrapper: &mcadApi.AppWrapper{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "a-job-with-cluster",
					Namespace: "a-job-with-cluster",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: mcadApi.AppWrapperSpec{
					AggrResources: mcadApi.AppWrapperResourceList{
						GenericItems: []mcadApi.AppWrapperGenericResource{
							{
								CustomPodResources: []mcadApi.CustomPodResourceTemplate{
									{
										Replicas: 1,
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("2200m"),
											"memory": resource.MustParse("512Mi"),
										},
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("4200m"),
											"memory": resource.MustParse("768Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			Name: "A job with a cluster selector",
			inputJob: &rayv1api.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "a-job-without-cluster",
					Namespace: "a-job-without-cluster",
				},
				Spec: rayv1api.RayJobSpec{
					Entrypoint: "/opt/world-domination.py",
					ClusterSelector: map[string]string{
						"ray.io/cluster": "some-test-cluster",
					},
				},
			},
			expectedAppwrapper: &mcadApi.AppWrapper{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "a-job-without-cluster",
					Namespace: "a-job-without-cluster",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: mcadApi.AppWrapperSpec{
					AggrResources: mcadApi.AppWrapperResourceList{
						GenericItems: []mcadApi.AppWrapperGenericResource{
							{
								CustomPodResources: []mcadApi.CustomPodResourceTemplate{
									{
										Replicas: 1,
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("200m"),
											"memory": resource.MustParse("256Mi"),
										},
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("200m"),
											"memory": resource.MustParse("256Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			Name: "A job with a cluster selector and with a submitter speck",
			inputJob: &rayv1api.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "a-job-without-cluster",
					Namespace: "a-job-without-cluster",
				},
				Spec: rayv1api.RayJobSpec{
					Entrypoint: "/opt/world-domination.py",
					ClusterSelector: map[string]string{
						"ray.io/cluster": "some-test-cluster",
					},
					SubmitterPodTemplate: &v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("100m"),
											"memory": resource.MustParse("16Mi"),
										},
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("100m"),
											"memory": resource.MustParse("16Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedAppwrapper: &mcadApi.AppWrapper{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "a-job-without-cluster",
					Namespace: "a-job-without-cluster",
					Labels: map[string]string{
						util.KubernetesManagedByLabelKey: util.ComponentName,
					},
				},
				Spec: mcadApi.AppWrapperSpec{
					AggrResources: mcadApi.AppWrapperResourceList{
						GenericItems: []mcadApi.AppWrapperGenericResource{
							{
								CustomPodResources: []mcadApi.CustomPodResourceTemplate{
									{
										Replicas: 1,
										Requests: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("100m"),
											"memory": resource.MustParse("16Mi"),
										},
										Limits: map[v1.ResourceName]resource.Quantity{
											"cpu":    resource.MustParse("100m"),
											"memory": resource.MustParse("16Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
	}
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualAppwrapper, err := appwrapperBuilder.AppWrapperFromRayJob(tc.inputJob)
			if tc.expectedError == nil {
				require.NotNil(t, actualAppwrapper, "An actual appwrapper is expected")
				compareAppWrappers(t, tc.expectedAppwrapper, actualAppwrapper)
			} else {
				require.EqualError(t, err, tc.expectedError.Error(), "Matching error expected")
			}
		})
	}
}

func setupScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	err := rayv1api.AddToScheme(scheme)
	require.NoError(t, err, "No error expected while adding RayAPI to the scheme")

	err = mcadApi.AddToScheme(scheme)
	require.NoError(t, err, "No error encountered while adding MCAD API to the scheme")
	return scheme
}

func compareAppWrappers(t *testing.T, expected, actual *mcadApi.AppWrapper) {
	failed := false
	diffs, err := diff.Diff(expected, actual,
		diff.SliceOrdering(true),
		diff.ConvertCompatibleTypes(),
		diff.StructMapKeySupport(),
		diff.CustomValueDiffers(&QuantityDiffer{}),
		diff.Filter(func(path []string, parent reflect.Type, field reflect.StructField) bool {
			return !(parent.Name() == "AppWrapperGenericResource" && field.Name == "GenericTemplate")
		}))
	if assert.NoError(t, err) && len(diffs) > 0 {
		t.Logf("No differences expected in Spec expected, yet there are some...")
		dumpChangeLog(t, diffs)
		failed = failed || true
	}
	if failed {
		t.Fail()
	}
}

func dumpChangeLog(t *testing.T, diffs diff.Changelog) {
	for _, diff := range diffs {
		t.Logf("Differences found in field: %s, expected: %#v actual: %#v", strings.Join(diff.Path, "."), diff.From, diff.To)
	}
}

type QuantityDiffer struct {
	DiffFunc (func(path []string, a, b reflect.Value, p interface{}) error)
}

func (d *QuantityDiffer) Diff(dt diff.DiffType, df diff.DiffFunc, cl *diff.Changelog, path []string, a, b reflect.Value, parent interface{}) error {
	if a.Kind() == reflect.Invalid {
		cl.Add(diff.CREATE, path, nil, b.Interface())
		return nil
	}
	if b.Kind() == reflect.Invalid {
		cl.Add(diff.DELETE, path, a.Interface(), nil)
		return nil
	}
	quantityA, ok := a.Interface().(resource.Quantity)
	if !ok {
		return errors.New("failed to convert interface a to a resource.Quantity")
	}
	quantityB, ok := b.Interface().(resource.Quantity)
	if !ok {
		return errors.New("failed to convert interface b to a resource.Quantity")
	}
	if quantityA.Value() != quantityB.Value() {
		cl.Add(diff.UPDATE, path, quantityA, quantityB)
	}
	return nil
}

func (differ *QuantityDiffer) Match(a, b reflect.Value) bool {
	return diff.AreType(a, b, reflect.TypeOf(resource.Quantity{}))
}

func (differ *QuantityDiffer) InsertParentDiffer(dfunc func(path []string, a, b reflect.Value, p interface{}) error) {
	differ.DiffFunc = dfunc
}
