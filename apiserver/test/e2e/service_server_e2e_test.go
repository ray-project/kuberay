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
					Version:                            tCtx.GetRayVersion(),
					ServeConfig_V2:                     "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 2\n        max_replicas_per_node: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n",
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
			actualService, actualRPCStatus, err := tCtx.GetRayAPIServerClient().CreateRayService(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRPCStatus, "No RPC status expected")
				require.NotNil(t, actualService, "A service is expected")
				waitForRunningService(t, tCtx, actualService.Name)
				tCtx.DeleteRayService(t, actualService.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRPCStatus, "A not nill RPC status is required")
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
			actualRPCStatus, err := tCtx.GetRayAPIServerClient().DeleteRayService(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRPCStatus, "No RPC status expected")
				waitForDeletedService(t, tCtx, testServiceRequest.Service.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRPCStatus, "A not nill RPC status is required")
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

	response, actualRPCStatus, err := tCtx.GetRayAPIServerClient().ListAllRayServices()
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRPCStatus, "No RPC status expected")
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

	response, actualRPCStatus, err := tCtx.GetRayAPIServerClient().ListRayServices(&api.ListRayServicesRequest{
		Namespace: tCtx.GetNamespaceName(),
	})
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRPCStatus, "No RPC status expected")
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
			actualService, actualRPCStatus, err := tCtx.GetRayAPIServerClient().GetRayService(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRPCStatus, "No RPC status expected")
				require.Equal(t, tc.Input.Name, actualService.Name)
				require.Equal(t, tCtx.GetNamespaceName(), actualService.Namespace)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRPCStatus, "A not nill RPC status is required")
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
			Version:                            tCtx.GetRayVersion(),
			ServeConfig_V2:                     "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 2\n        max_replicas_per_node: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n",
			ServiceUnhealthySecondThreshold:    10,
			DeploymentUnhealthySecondThreshold: 20,
			ClusterSpec:                        clusterSpec,
		},
		Namespace: tCtx.GetNamespaceName(),
	}
	actualService, actualRPCStatus, err := tCtx.GetRayAPIServerClient().CreateRayService(testServiceRequest)
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRPCStatus, "No RPC status expected")
	require.NotNil(t, actualService, "A service is expected")
	waitForRunningService(t, tCtx, actualService.Name)

	return testServiceRequest
}

func waitForRunningService(t *testing.T, tCtx *End2EndTestingContext, serviceName string) {
	// wait for the service to be in a running state for 3 minutes
	// if is not in that state, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayService, err := tCtx.GetRayServiceByName(serviceName)
		if err != nil {
			return true, err
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
		rayService, err := tCtx.GetRayServiceByName(serviceName)
		if err != nil &&
			assert.EqualError(t, err, "rayservices.ray.io \""+serviceName+"\" not found") {
			return true, nil
		}
		t.Logf("Found status of '%s' for ray service '%s'", rayService.Status.ServiceStatus, serviceName)
		return false, err
	})
	require.NoErrorf(t, err, "No error expected when deleting ray service: '%s', err %v", serviceName, err)
}
