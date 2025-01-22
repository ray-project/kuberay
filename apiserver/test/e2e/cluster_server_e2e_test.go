package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	kuberayHTTP "github.com/ray-project/kuberay/apiserver/pkg/http"
	api "github.com/ray-project/kuberay/proto/go_client"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"

	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

// TestCreateClusterEndpoint sequentially iterates over the create cluster endpoint
// with valid and invalid requests
func TestCreateClusterEndpoint(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	// create config map and register a cleanup hook upon success
	configMapName := tCtx.CreateConfigMap(t, map[string]string{
		"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
	})
	t.Cleanup(func() {
		tCtx.DeleteConfigMap(t, configMapName)
	})

	tests := []GenericEnd2EndTest[*api.CreateClusterRequest]{
		{
			Name: "Create a cluster without volumes",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "3cpo",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: tCtx.GetComputeTemplateName(),
							Image:           tCtx.GetRayImage(),
							ServiceType:     "NodePort",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{
							{
								GroupName:       "small-wg",
								ComputeTemplate: tCtx.GetComputeTemplateName(),
								Image:           tCtx.GetRayImage(),
								Replicas:        1,
								MinReplicas:     1,
								MaxReplicas:     5,
								RayStartParams: map[string]string{
									"node-ip-address": "$MY_POD_IP",
								},
							},
						},
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Create cluster with config map volume",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "boris",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: tCtx.GetComputeTemplateName(),
							Image:           tCtx.GetRayImage(),
							ServiceType:     "NodePort",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
							Volumes: []*api.Volume{
								{
									MountPath:  "/home/ray/samples",
									VolumeType: api.Volume_CONFIGMAP,
									Name:       "code-sample",
									Source:     tCtx.GetConfigMapName(),
									Items: map[string]string{
										"counter_sample.py": "counter_sample.py",
									},
								},
							},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{
							{
								GroupName:       "small-wg",
								ComputeTemplate: tCtx.GetComputeTemplateName(),
								Image:           tCtx.GetRayImage(),
								Replicas:        1,
								MinReplicas:     1,
								MaxReplicas:     5,
								RayStartParams: map[string]string{
									"node-ip-address": "$MY_POD_IP",
								},
								Volumes: []*api.Volume{
									{
										MountPath:  "/home/ray/samples",
										VolumeType: api.Volume_CONFIGMAP,
										Name:       "code-sample",
										Source:     tCtx.GetConfigMapName(),
										Items: map[string]string{
											"counter_sample.py": "counter_sample.py",
										},
									},
								},
							},
						},
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Create cluster with no workers",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "boris",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: tCtx.GetComputeTemplateName(),
							Image:           tCtx.GetRayImage(),
							ServiceType:     "NodePort",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
							Volumes: []*api.Volume{
								{
									MountPath:  "/home/ray/samples",
									VolumeType: api.Volume_CONFIGMAP,
									Name:       "code-sample",
									Source:     tCtx.GetConfigMapName(),
									Items: map[string]string{
										"counter_sample.py": "counter_sample.py",
									},
								},
							},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{},
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Create cluster with no namespace in request",
			Input: &api.CreateClusterRequest{
				Cluster:   &api.Cluster{},
				Namespace: "",
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Create cluster with no cluster object",
			Input: &api.CreateClusterRequest{
				Cluster:   nil,
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with no namespace in the cluster object",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   "",
					User:        "",
					Version:     "",
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with no name in the cluster object",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        "",
					Namespace:   tCtx.GetNamespaceName(),
					User:        "",
					Version:     "",
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with no user name in the cluster object",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "",
					Version:     "",
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with nil cluster spec in the cluster object",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "bullwinkle",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: nil,
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with no head group spec in the cluster object",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "bullwinkle",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with no compute template in the head group spec",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "boris",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: "",
							Image:           tCtx.GetRayImage(),
							ServiceType:     "NodePort",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{},
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with no ray start parameters in the head group spec",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "boris",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: tCtx.GetComputeTemplateName(),
							Image:           tCtx.GetRayImage(),
							ServiceType:     "NodePort",
							RayStartParams:  map[string]string{},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{},
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with no group name in the worker group spec",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "boris",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: tCtx.GetComputeTemplateName(),
							Image:           tCtx.GetRayImage(),
							ServiceType:     "NodePort",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{
							{},
						},
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with no compute template in the worker group spec",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "boris",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: tCtx.GetComputeTemplateName(),
							Image:           tCtx.GetRayImage(),
							ServiceType:     "NodePort",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{
							{
								GroupName:       "small-wg",
								ComputeTemplate: "",
								Image:           "",
							},
						},
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create cluster with invalid replica count in the worker group spec",
			Input: &api.CreateClusterRequest{
				Cluster: &api.Cluster{
					Name:        tCtx.GetNextName(),
					Namespace:   tCtx.GetNamespaceName(),
					User:        "boris",
					Version:     tCtx.GetRayVersion(),
					Environment: api.Cluster_DEV,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: tCtx.GetComputeTemplateName(),
							Image:           tCtx.GetRayImage(),
							ServiceType:     "NodePort",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{
							{
								GroupName:       "small-wg",
								ComputeTemplate: tCtx.GetComputeTemplateName(),
								Image:           tCtx.GetRayImage(),
							},
						},
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualCluster, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateCluster(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.NotNil(t, actualCluster, "A cluster is expected")
				waitForRunningCluster(t, tCtx, actualCluster.Name)
				tCtx.DeleteRayCluster(t, actualCluster.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func TestDeleteCluster(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	tCtx.CreateRayClusterWithConfigMaps(t, map[string]string{
		"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
		"fail_fast.py":      ReadFileAsString(t, "resources/fail_fast_sample.py"),
	})
	tests := []GenericEnd2EndTest[*api.DeleteClusterRequest]{
		{
			Name: "Delete an existing cluster",
			Input: &api.DeleteClusterRequest{
				Name:      tCtx.GetRayClusterName(),
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Delete a non existing cluster",
			Input: &api.DeleteClusterRequest{
				Name:      "bogus-cluster-name",
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Delete a cluster with no namespace",
			Input: &api.DeleteClusterRequest{
				Name:      "bogus-cluster-name",
				Namespace: "",
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Delete a cluster with no name",
			Input: &api.DeleteClusterRequest{
				Name:      "",
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualRpcStatus, err := tCtx.GetRayApiServerClient().DeleteCluster(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				waitForDeletedCluster(t, tCtx, tc.Input.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

// TestGetAllClusters tests gets all Ray clusters from k8s cluster
func TestGetAllClusters(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	actualCluster, confiMapName := tCtx.CreateRayClusterWithConfigMaps(t, map[string]string{
		"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
		"fail_fast.py":      ReadFileAsString(t, "resources/fail_fast_sample.py"),
	})
	t.Cleanup(func() {
		tCtx.DeleteRayCluster(t, actualCluster.Name)
		tCtx.DeleteConfigMap(t, confiMapName)
	})

	response, actualRpcStatus, err := tCtx.GetRayApiServerClient().ListAllClusters()
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, response, "A response is expected")
	require.NotEmpty(t, response.Clusters, "A list of clusters is required")
	gotCluster := false
	for _, cluster := range response.Clusters {
		if tCtx.GetRayClusterName() == cluster.Name && tCtx.GetNamespaceName() == cluster.Namespace {
			gotCluster = true
			break
		}
	}
	if !gotCluster {
		t.Error("Getting all clusters did not return expected one")
	}
}

// TestGetClustersInNamespace validates t
func TestGetClustersInNamespace(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	cluster, configMapName := tCtx.CreateRayClusterWithConfigMaps(t, map[string]string{
		"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
		"fail_fast.py":      ReadFileAsString(t, "resources/fail_fast_sample.py"),
	})
	t.Cleanup(func() {
		tCtx.DeleteRayCluster(t, cluster.Name)
		tCtx.DeleteConfigMap(t, configMapName)
	})

	response, actualRpcStatus, err := tCtx.GetRayApiServerClient().ListClusters(
		&api.ListClustersRequest{
			Namespace: tCtx.GetNamespaceName(),
		})
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, response, "A response is expected")
	require.NotEmpty(t, response.Clusters, "A list of compute templates is required")
	gotCluster := false
	for _, cluster := range response.Clusters {
		if tCtx.GetRayClusterName() == cluster.Name && tCtx.GetNamespaceName() == cluster.Namespace {
			gotCluster = true
			break
		}
	}
	if !gotCluster {
		t.Error("Getting clusters din namespace did not return expected one")
	}
}

// TestDeleteTemplate sequentially iterates over the delete compute template endpoint
// to validate various input scenarios
func TestGetClustersByNameInNamespace(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	cluster, configMapName := tCtx.CreateRayClusterWithConfigMaps(t, map[string]string{
		"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
		"fail_fast.py":      ReadFileAsString(t, "resources/fail_fast_sample.py"),
	})
	t.Cleanup(func() {
		tCtx.DeleteRayCluster(t, cluster.Name)
		tCtx.DeleteConfigMap(t, configMapName)
	})

	tests := []GenericEnd2EndTest[*api.GetClusterRequest]{
		{
			Name: "Get cluster by name in a namespace",
			Input: &api.GetClusterRequest{
				Name:      tCtx.GetRayClusterName(),
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Get non existing cluster",
			Input: &api.GetClusterRequest{
				Name:      "a-bogus-cluster-name",
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Get a cluster with no Name",
			Input: &api.GetClusterRequest{
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Get a cluster with no namespace",
			Input: &api.GetClusterRequest{
				Name: "some-Name",
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualCluster, actualRpcStatus, err := tCtx.GetRayApiServerClient().GetCluster(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.Equal(t, tCtx.GetRayClusterName(), actualCluster.Name)
				require.Equal(t, tCtx.GetNamespaceName(), actualCluster.Namespace)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func waitForRunningCluster(t *testing.T, tCtx *End2EndTestingContext, clusterName string) {
	// wait for the cluster to be in a running state for 3 minutes
	// if is not in that state, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayCluster, err00 := tCtx.GetRayClusterByName(clusterName)
		if err00 != nil {
			return true, err00
		}
		t.Logf("Found cluster state of '%s' for ray cluster '%s'", rayCluster.Status.State, clusterName)
		return rayCluster.Status.State == rayv1api.Ready, nil
	})
	require.NoErrorf(t, err, "No error expected when getting ray cluster: '%s', err %v", tCtx.GetRayClusterName(), err)
}

func waitForDeletedCluster(t *testing.T, tCtx *End2EndTestingContext, clusterName string) {
	// wait for the cluster to be deleted
	// if is not in that state, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayCluster, err00 := tCtx.GetRayClusterByName(clusterName)
		if err00 != nil &&
			assert.EqualError(t, err00, "rayclusters.ray.io \""+tCtx.GetRayClusterName()+"\" not found") {
			return true, nil
		}
		t.Logf("Found status of '%s' for ray cluster '%s'", rayCluster.Status.State, clusterName)
		return false, err00
	})
	require.NoErrorf(t, err, "No error expected when deleting ray cluster: '%s', err %v", clusterName, err)
}
