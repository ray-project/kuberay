package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	kuberayHTTP "github.com/ray-project/kuberay/apiserver/pkg/http"
	api "github.com/ray-project/kuberay/proto/go_client"
	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"
)

var serveConfigs = []*api.ServeConfig{
	{
		DeploymentName: "OrangeStand",
		Replicas:       1,
		UserConfig:     "price: 2",
		ActorOptions: &api.ActorOptions{
			CpusPerActor: 0.1,
		},
	},
	{
		DeploymentName: "PearStand",
		Replicas:       1,
		UserConfig:     "price: 1",
		ActorOptions: &api.ActorOptions{
			CpusPerActor: 0.1,
		},
	},
	{
		DeploymentName: "FruitMarket",
		Replicas:       1,
		ActorOptions: &api.ActorOptions{
			CpusPerActor: 0.1,
		},
	},
	{
		DeploymentName: "DAGDriver",
		Replicas:       1,
		RoutePrefix:    "/",
		ActorOptions: &api.ActorOptions{
			CpusPerActor: 0.1,
		},
	},
}

// TestServiceServerV2 sequentially iterates over the endpoints of the service endpoints using
// V2 configurations (yaml)
func TestCreateServiceV2(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	clusterSpec := &api.ClusterSpec{
		HeadGroupSpec: &api.HeadGroupSpec{
			ComputeTemplate: tCtx.GetComputeTemplateName(),
			Image:           tCtx.GetRayImage(),
			ServiceType:     "NodePort",
			EnableIngress:   false,
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
					"dashboard-host":      "0.0.0.0",
					"metrics-export-port": "8080",
				},
			},
		},
	}

	tests := []GenericEnd2EndTest[*api.CreateRayServiceRequest]{
		{
			Name: "Create a fruit stand ray service using V2 configuration",
			Input: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Name:                               tCtx.GetNextName(),
					Namespace:                          tCtx.GetNamespaceName(),
					User:                               "user1",
					ServeConfig_V2:                     "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: DAGDriver\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n      - name: create_order\n        num_replicas: 1\n      - name: DAGDriver\n        num_replicas: 1\n",
					ServiceUnhealthySecondThreshold:    10,
					DeploymentUnhealthySecondThreshold: 20,
					ClusterSpec:                        clusterSpec,
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Create a service request with no namespace value",
			Input: &api.CreateRayServiceRequest{
				Service:   &api.RayService{},
				Namespace: "",
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Create a service request with mismatching namespaces",
			Input: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Namespace: "another-namespace-name",
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create a service request with no name",
			Input: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Namespace: tCtx.GetNamespaceName(),
					Name:      "",
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create a service request with no user",
			Input: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Namespace: tCtx.GetNamespaceName(),
					Name:      tCtx.GetNextName(),
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create a service request with no cluster spec",
			Input: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Namespace:   tCtx.GetNamespaceName(),
					Name:        tCtx.GetNextName(),
					ClusterSpec: nil,
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
			actualService, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateRayService(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.NotNil(t, actualService, "A service is expected")
				waitForRunningService(t, tCtx, actualService.Name)
				tCtx.DeleteRayService(t, actualService.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func TestCreateServiceV1(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	startParams := map[string]string{
		"dashboard-host":      "0.0.0.0",
		"metrics-export-port": "8080",
	}
	clusterSpec := &api.ClusterSpec{
		HeadGroupSpec: &api.HeadGroupSpec{
			ComputeTemplate: tCtx.GetComputeTemplateName(),
			Image:           tCtx.GetRayImage(),
			ServiceType:     "NodePort",
			EnableIngress:   false,
			RayStartParams:  startParams,
		},
		WorkerGroupSpec: []*api.WorkerGroupSpec{
			{
				GroupName:       "small-wg",
				ComputeTemplate: tCtx.GetComputeTemplateName(),
				Image:           tCtx.GetRayImage(),
				Replicas:        1,
				MinReplicas:     1,
				MaxReplicas:     5,
				RayStartParams:  startParams,
			},
		},
	}
	tests := []GenericEnd2EndTest[*api.CreateRayServiceRequest]{
		{
			Name: "Create a fruit stand service V1",
			Input: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Name:      tCtx.GetNextName(),
					Namespace: tCtx.GetNamespaceName(),
					User:      "user1",
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath:   "fruit.deployment_graph",
						RuntimeEnv:   "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
						ServeConfigs: serveConfigs,
					},
					ServiceUnhealthySecondThreshold:    300,
					DeploymentUnhealthySecondThreshold: 900,
					ClusterSpec:                        clusterSpec,
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Create a V1 serve service with empty deployment graph spec",
			Input: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Name:      tCtx.GetNextName(),
					Namespace: tCtx.GetNamespaceName(),
					User:      "user1",
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath:   "fruit.deployment_graph",
						RuntimeEnv:   "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
						ServeConfigs: serveConfigs,
					},
					ServiceUnhealthySecondThreshold:    30,
					DeploymentUnhealthySecondThreshold: 90,
					ClusterSpec:                        &api.ClusterSpec{},
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
			actualService, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateRayService(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.NotNil(t, actualService, "A service is expected")
				waitForRunningService(t, tCtx, actualService.Name)
				tCtx.DeleteRayService(t, actualService.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func TestDeleteService(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testServiceRequest := createTestServiceV2(t, tCtx)

	tests := []GenericEnd2EndTest[*api.DeleteRayServiceRequest]{
		{
			Name: "Delete an existing service",
			Input: &api.DeleteRayServiceRequest{
				Name:      testServiceRequest.Service.Name,
				Namespace: testServiceRequest.Namespace,
			},
			ExpectedError: nil,
		},
		{
			Name: "Delete a non existing service",
			Input: &api.DeleteRayServiceRequest{
				Name:      "a-bogus-job-name",
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Delete a service without providing a namespace",
			Input: &api.DeleteRayServiceRequest{
				Name:      testServiceRequest.Service.Name,
				Namespace: "",
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
			actualRpcStatus, err := tCtx.GetRayApiServerClient().DeleteRayService(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				waitForDeletedService(t, tCtx, testServiceRequest.Service.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func TestGetAllServices(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testServiceRequest := createTestServiceV2(t, tCtx)
	t.Cleanup(func() {
		tCtx.DeleteRayService(t, testServiceRequest.Service.Name)
	})

	response, actualRpcStatus, err := tCtx.GetRayApiServerClient().ListAllRayServices()
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, response, "A response is expected")
	require.NotEmpty(t, response.Services, "A list of services is required")
	require.Equal(t, testServiceRequest.Service.Name, response.Services[0].Name)
	require.Equal(t, tCtx.GetNamespaceName(), response.Services[0].Namespace)
}

func TestGetServicesInNamespace(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testServiceRequest := createTestServiceV2(t, tCtx)
	t.Cleanup(func() {
		tCtx.DeleteRayService(t, testServiceRequest.Service.Name)
	})

	response, actualRpcStatus, err := tCtx.GetRayApiServerClient().ListRayServices(&api.ListRayServicesRequest{
		Namespace: tCtx.GetNamespaceName(),
	})
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, response, "A response is expected")
	require.NotEmpty(t, response.Services, "A list of compute templates is required")
	require.Equal(t, testServiceRequest.Service.Name, response.Services[0].Name)
	require.Equal(t, tCtx.GetNamespaceName(), response.Services[0].Namespace)
}

func TestGetService(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testServiceRequest := createTestServiceV2(t, tCtx)
	t.Cleanup(func() {
		tCtx.DeleteRayService(t, testServiceRequest.Service.Name)
	})
	tests := []GenericEnd2EndTest[*api.GetRayServiceRequest]{
		{
			Name: "Get job by name in a namespace",
			Input: &api.GetRayServiceRequest{
				Name:      testServiceRequest.Service.Name,
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Get non existing job",
			Input: &api.GetRayServiceRequest{
				Name:      "a-bogus-cluster-name",
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Get a job with no Name",
			Input: &api.GetRayServiceRequest{
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Get a job with no namespace",
			Input: &api.GetRayServiceRequest{
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
			actualService, actualRpcStatus, err := tCtx.GetRayApiServerClient().GetRayService(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.Equal(t, tc.Input.Name, actualService.Name)
				require.Equal(t, tCtx.GetNamespaceName(), actualService.Namespace)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func TestServiceServerV1Update(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testServiceRequest := createTestServiceV1(t, tCtx)
	t.Cleanup(func() {
		tCtx.DeleteRayService(t, testServiceRequest.Service.Name)
	})

	tests := []GenericEnd2EndTest[*api.UpdateRayServiceRequest]{
		{
			Name: "Update a fruit stand service V1 actor actions with no name",
			Input: &api.UpdateRayServiceRequest{
				Service: &api.RayService{
					Name: "",
				},
				Namespace: tCtx.GetNamespaceName(),
				Name:      tCtx.GetNextName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Update a fruit stand service V1 actor actions with no namespace name",
			Input: &api.UpdateRayServiceRequest{
				Service: &api.RayService{
					Name: tCtx.GetNextName(),
				},
				Namespace: tCtx.GetNamespaceName(),
				Name:      tCtx.GetCurrentName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Update a fruit stand service V1 actor actions with no user",
			Input: &api.UpdateRayServiceRequest{
				Service: &api.RayService{
					Namespace: tCtx.GetNamespaceName(),
					Name:      tCtx.GetNextName(),
					User:      "",
				},
				Namespace: tCtx.GetNamespaceName(),
				Name:      tCtx.GetCurrentName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Update a fruit stand service V1 actor actions with nil graph spec",
			Input: &api.UpdateRayServiceRequest{
				Service: &api.RayService{
					Namespace: tCtx.GetNamespaceName(),
					Name:      testServiceRequest.Service.Name,
					User:      testServiceRequest.Service.User,
				},
				Namespace: tCtx.GetNamespaceName(),
				Name:      testServiceRequest.Service.Name,
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Update a fruit stand service V1 actor actions with empty graph spec",
			Input: &api.UpdateRayServiceRequest{
				Service: &api.RayService{
					Namespace:                tCtx.GetNamespaceName(),
					Name:                     testServiceRequest.Service.Name,
					User:                     testServiceRequest.Service.User,
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{},
				},
				Namespace: tCtx.GetNamespaceName(),
				Name:      testServiceRequest.Service.Name,
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Update a fruit stand service V1 actor actions with no cluster spec",
			Input: &api.UpdateRayServiceRequest{
				Service: &api.RayService{
					Namespace: testServiceRequest.Service.Namespace,
					Name:      testServiceRequest.Service.Name,
					User:      testServiceRequest.Service.User,
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath:   "fruit.deployment_graph",
						ServeConfigs: []*api.ServeConfig{},
					},
				},
				Namespace: testServiceRequest.Service.Namespace,
				Name:      testServiceRequest.Service.Name,
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		// TODO @z103cb this test is failing, needs to be investigated to determine if is a valid test,
		// the cluster fails to come up.
		/*
			{
				Name: "Update a fruit stand service V1 actor actions with cluster and no serve configs",
				Input: &api.UpdateRayServiceRequest{
					Service: &api.RayService{
						Namespace: testServiceRequest.Service.Namespace,
						Name:      testServiceRequest.Service.Name,
						User:      testServiceRequest.Service.User,
						ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
							ImportPath:   "fruit.deployment_graph",
							ServeConfigs: []*api.ServeConfig{},
						},
						ClusterSpec: testServiceRequest.Service.ClusterSpec,
					},
					Namespace: testServiceRequest.Service.Namespace,
					Name:      testServiceRequest.Service.Name,
				},
				ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
					HTTPStatusCode: http.StatusBadRequest,
				},
			},
		*/
		{
			Name: "Update a fruit stand service V1 actor actions",
			Input: &api.UpdateRayServiceRequest{
				Service: &api.RayService{
					Namespace:                          testServiceRequest.Service.Namespace,
					Name:                               testServiceRequest.Service.Name,
					User:                               testServiceRequest.Service.User,
					ServiceUnhealthySecondThreshold:    90,
					DeploymentUnhealthySecondThreshold: 30,
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath: "fruit.deployment_graph",
						RuntimeEnv: "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
						ServeConfigs: []*api.ServeConfig{
							{
								DeploymentName: "OrangeStand",
								Replicas:       1,
								UserConfig:     "price: 2",
								ActorOptions: &api.ActorOptions{
									CpusPerActor: 0.2,
								},
							},
							{
								DeploymentName: "PearStand",
								Replicas:       1,
								UserConfig:     "price: 1",
								ActorOptions: &api.ActorOptions{
									CpusPerActor: 0.2,
								},
							},
							{
								DeploymentName: "FruitMarket",
								Replicas:       1,
								ActorOptions: &api.ActorOptions{
									CpusPerActor: 0.2,
								},
							},
							{
								DeploymentName: "DAGDriver",
								Replicas:       1,
								RoutePrefix:    "/",
								ActorOptions: &api.ActorOptions{
									CpusPerActor: 0.2,
								},
							},
						},
					},
					ClusterSpec: testServiceRequest.Service.ClusterSpec,
				},
				Namespace: testServiceRequest.Service.Namespace,
				Name:      testServiceRequest.Service.Name,
			},
			ExpectedError: nil,
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualService, actualRpcStatus, err := tCtx.GetRayApiServerClient().UpdateRayService(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.NotNil(t, actualService, "A service is expected")
				waitForRunningService(t, tCtx, actualService.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})

	}
}

func TestServiceServerV1Patch(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testServiceRequest := createTestServiceV1(t, tCtx)
	t.Cleanup(func() {
		tCtx.DeleteRayService(t, testServiceRequest.Service.Name)
	})

	tests := []GenericEnd2EndTest[*api.UpdateRayServiceConfigsRequest]{
		{
			Name: "Update service cluster worker group",
			Input: &api.UpdateRayServiceConfigsRequest{
				Name:      testServiceRequest.Service.Name,
				Namespace: testServiceRequest.Service.Namespace,
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{
						{
							GroupName:   "small-wg",
							Replicas:    2,
							MinReplicas: 2,
							MaxReplicas: 10,
						},
					},
				},
			},
			ExpectedError: nil,
		},
		{
			Name: "Update service cluster worker group with no values",
			Input: &api.UpdateRayServiceConfigsRequest{
				Name:      testServiceRequest.Service.Name,
				Namespace: testServiceRequest.Service.Namespace,
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{},
				},
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Update service cluster worker group with no name",
			Input: &api.UpdateRayServiceConfigsRequest{
				Name:      testServiceRequest.Service.Name,
				Namespace: testServiceRequest.Service.Namespace,
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{
						{},
					},
				},
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Update service cluster worker group with no name and valid replicas",
			Input: &api.UpdateRayServiceConfigsRequest{
				Name:      testServiceRequest.Service.Name,
				Namespace: testServiceRequest.Service.Namespace,
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{
						{
							GroupName:   "",
							Replicas:    4,
							MinReplicas: 4,
							MaxReplicas: 12,
						},
					},
				},
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
			actualService, actualRpcStatus, err := tCtx.GetRayApiServerClient().UpdateRayServiceConfigs(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.NotNil(t, actualService, "A service is expected")
				waitForRunningServiceWithWorkGroupSpec(t, tCtx, actualService.Name, tc.Input.UpdateService.WorkerGroupUpdateSpec[0].MinReplicas,
					tc.Input.UpdateService.WorkerGroupUpdateSpec[0].MaxReplicas, tc.Input.UpdateService.WorkerGroupUpdateSpec[0].Replicas)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func createTestServiceV2(t *testing.T, tCtx *End2EndTestingContext) *api.CreateRayServiceRequest {
	clusterSpec := &api.ClusterSpec{
		HeadGroupSpec: &api.HeadGroupSpec{
			ComputeTemplate: tCtx.GetComputeTemplateName(),
			Image:           tCtx.GetRayImage(),
			ServiceType:     "NodePort",
			EnableIngress:   false,
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
					"dashboard-host":      "0.0.0.0",
					"metrics-export-port": "8080",
				},
			},
		},
	}

	testServiceRequest := &api.CreateRayServiceRequest{
		Service: &api.RayService{
			Name:                               tCtx.GetNextName(),
			Namespace:                          tCtx.GetNamespaceName(),
			User:                               "user1",
			ServeConfig_V2:                     "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: DAGDriver\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n      - name: create_order\n        num_replicas: 1\n      - name: DAGDriver\n        num_replicas: 1\n",
			ServiceUnhealthySecondThreshold:    10,
			DeploymentUnhealthySecondThreshold: 20,
			ClusterSpec:                        clusterSpec,
		},
		Namespace: tCtx.GetNamespaceName(),
	}
	actualService, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateRayService(testServiceRequest)
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, actualService, "A service is expected")
	waitForRunningService(t, tCtx, actualService.Name)

	return testServiceRequest
}

func createTestServiceV1(t *testing.T, tCtx *End2EndTestingContext) *api.CreateRayServiceRequest {
	clusterSpec := &api.ClusterSpec{
		HeadGroupSpec: &api.HeadGroupSpec{
			ComputeTemplate: tCtx.GetComputeTemplateName(),
			Image:           tCtx.GetRayImage(),
			ServiceType:     "NodePort",
			EnableIngress:   false,
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
					"dashboard-host":      "0.0.0.0",
					"metrics-export-port": "8080",
				},
			},
		},
	}

	testServiceRequest := &api.CreateRayServiceRequest{
		Service: &api.RayService{
			Name:      tCtx.GetNextName(),
			Namespace: tCtx.GetNamespaceName(),
			User:      "user1",
			ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
				ImportPath:   "fruit.deployment_graph",
				RuntimeEnv:   "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
				ServeConfigs: serveConfigs,
			},
			ServiceUnhealthySecondThreshold:    90,
			DeploymentUnhealthySecondThreshold: 30,
			ClusterSpec:                        clusterSpec,
		},
		Namespace: tCtx.GetNamespaceName(),
	}
	actualService, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateRayService(testServiceRequest)
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, actualService, "A service is expected")
	waitForRunningService(t, tCtx, actualService.Name)

	return testServiceRequest
}

func waitForRunningService(t *testing.T, tCtx *End2EndTestingContext, serviceName string) {
	// wait for the service to be in a running state for 3 minutes
	// if is not in that state, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayService, err00 := tCtx.GetRayServiceByName(serviceName)
		if err00 != nil {
			return true, err00
		}
		t.Logf("Found status of '%s' for ray service '%s'", rayService.Status.ServiceStatus, serviceName)
		return rayService.Status.ServiceStatus == rayv1api.Running, nil
	})
	require.NoErrorf(t, err, "No error expected when getting ray service: '%s', err %v", serviceName, err)
}

func waitForDeletedService(t *testing.T, tCtx *End2EndTestingContext, serviceName string) {
	// wait for the service to be deleted
	// if is not in that state, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayService, err00 := tCtx.GetRayServiceByName(serviceName)
		if err00 != nil &&
			assert.EqualError(t, err00, "rayservices.ray.io \""+serviceName+"\" not found") {
			return true, nil
		}
		t.Logf("Found status of '%s' for ray service '%s'", rayService.Status.ServiceStatus, serviceName)
		return false, err00
	})
	require.NoErrorf(t, err, "No error expected when deleting ray service: '%s', err %v", serviceName, err)
}

func waitForRunningServiceWithWorkGroupSpec(t *testing.T, tCtx *End2EndTestingContext, serviceName string, minWorkerReplicas, maxWorkerReplicas, availableWorkerReplicas int32) {
	// wait for the service to be in a running state for 3 minutes
	// if is not in that state, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayService, err00 := tCtx.GetRayServiceByName(serviceName)
		if err00 != nil {
			return true, err00
		}
		t.Logf("Found status of '%s' for ray service '%s'", rayService.Status.ServiceStatus, serviceName)
		if rayService.Status.ServiceStatus == rayv1api.Running {
			rayCluster, err := tCtx.GetRayClusterByName(rayService.Status.ActiveServiceStatus.RayClusterName)
			require.NoErrorf(t, err, "Expecting no error when getting cluster named: '%s'",
				rayService.Status.ActiveServiceStatus.RayClusterName)
			t.Logf("Ray service cluster state is: MinWorkerReplicas = %d && MaxWorkerReplicas == %d && AvailableWorkerReplicas == %d",
				rayCluster.Status.MinWorkerReplicas, rayCluster.Status.MaxWorkerReplicas, rayCluster.Status.AvailableWorkerReplicas)
			if rayCluster.Status.MinWorkerReplicas == minWorkerReplicas &&
				rayCluster.Status.MaxWorkerReplicas == maxWorkerReplicas &&
				rayCluster.Status.AvailableWorkerReplicas == availableWorkerReplicas {
				return true, nil
			}
		}
		return false, nil
	})
	require.NoErrorf(t, err, "No error expected when getting ray service: '%s', err %v", serviceName, err)
}
