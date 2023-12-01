package server_test

import (
	"testing"

	"github.com/ray-project/kuberay/apiserver/pkg/server"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/stretchr/testify/require"
)

func TestValidateClusterSpec(t *testing.T) {
	tests := []struct {
		name          string
		clusterSpec   *api.ClusterSpec
		expectedError error
	}{
		{
			name: "A valid cluster spec",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{
						GroupName:       "group-1",
						ComputeTemplate: "group-1-template",
						Replicas:        1,
						MinReplicas:     1,
						MaxReplicas:     1,
					},
					{
						GroupName:       "group-2",
						ComputeTemplate: "group-2-template",
						Replicas:        1,
						MinReplicas:     1,
						MaxReplicas:     1,
					},
				},
			},
			expectedError: nil,
		},
		{
			name:          "A nill cluster spec",
			clusterSpec:   nil,
			expectedError: util.NewInvalidInputError("A ClusterSpec object is required. Please specify one."),
		},
		{
			name:          "An empty cluster spec",
			clusterSpec:   &api.ClusterSpec{},
			expectedError: util.NewInvalidInputError("Cluster Spec Object requires HeadGroupSpec to be populated. Please specify one."),
		},
		{
			name: "An empty head group cluster spec",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec:   &api.HeadGroupSpec{},
				WorkerGroupSpec: []*api.WorkerGroupSpec{},
			},
			expectedError: util.NewInvalidInputError("HeadGroupSpec compute template is empty. Please specify a valid value."),
		},
		{
			name: "A head group without ray start parameters",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams:  nil,
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{},
			},
			expectedError: util.NewInvalidInputError("HeadGroupSpec RayStartParams is empty. Please specify values."),
		},
		{
			name: "An empty worker group",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{},
			},
			expectedError: nil,
		},
		{
			name: "Two empty worker group specs",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{},
					{},
				},
			},
			expectedError: util.NewInvalidInputError("WorkerNodeSpec 0 group name is empty. Please specify a valid value."),
		},
		{
			name: "A worker group spec without a group name",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{
						GroupName:       "",
						ComputeTemplate: "group-1-template",
						Replicas:        1,
						MinReplicas:     1,
						MaxReplicas:     1,
					},
				},
			},
			expectedError: util.NewInvalidInputError("WorkerNodeSpec 0 group name is empty. Please specify a valid value."),
		},
		{
			name: "A worker group spec without a template",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{
						GroupName:       "group 1",
						ComputeTemplate: "",
						Replicas:        1,
						MinReplicas:     1,
						MaxReplicas:     1,
					},
				},
			},
			expectedError: util.NewInvalidInputError("WorkerNodeSpec 0 compute template is empty. Please specify a valid value."),
		},
		{
			name: "A worker group spec with 0 max replicas",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{
						GroupName:       "group 1",
						ComputeTemplate: "a template",
						MaxReplicas:     0,
					},
				},
			},
			expectedError: util.NewInvalidInputError("WorkerNodeSpec 0 MaxReplicas can not be 0. Please specify a valid value."),
		},
		{
			name: "A worker group spec with invalid min replicas",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{
						GroupName:       "group 1",
						ComputeTemplate: "a template",
						MinReplicas:     5,
						MaxReplicas:     1,
					},
				},
			},
			expectedError: util.NewInvalidInputError("WorkerNodeSpec 0 MinReplica > MaxReplicas. Please specify a valid value."),
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			actualError := server.ValidateClusterSpec(tc.clusterSpec)
			if tc.expectedError == nil {
				require.NoError(t, actualError, "No error expected.")
			} else {
				require.EqualError(t, actualError, tc.expectedError.Error(), "A matching error is expected")
			}
		})
	}
}

func TestValidateCreateServiceRequest(t *testing.T) {
	tests := []struct {
		name          string
		request       *api.CreateRayServiceRequest
		expectedError error
	}{
		{
			name: "A valid create service request V2",
			request: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Name:                               "a-name",
					Namespace:                          "a-namespace",
					User:                               "a-user",
					ServeConfig_V2:                     "some yaml",
					ServiceUnhealthySecondThreshold:    900,
					DeploymentUnhealthySecondThreshold: 300,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: "a compute template name",
							EnableIngress:   false,
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
							Volumes: []*api.Volume{},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{
							{
								GroupName:       "group 1",
								ComputeTemplate: "a-template",
								Replicas:        1,
								MinReplicas:     1,
								MaxReplicas:     1,
							},
						},
					},
				},
				Namespace: "a-namespace",
			},
			expectedError: nil,
		},
		{
			name: "A valid create service request V1",
			request: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Name:      "a-name",
					Namespace: "a-namespace",
					User:      "a-user",
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath: "fruit.deployment_graph",
						RuntimeEnv: "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
						ServeConfigs: []*api.ServeConfig{
							{
								DeploymentName: "OrangeStand",
								Replicas:       1,
								UserConfig:     "price: 2",
								ActorOptions: &api.ActorOptions{
									CpusPerActor: 0.1,
								},
							},
						},
					},
					ServiceUnhealthySecondThreshold:    900,
					DeploymentUnhealthySecondThreshold: 300,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: "a compute template name",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
							Volumes: []*api.Volume{},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{
							{
								GroupName:       "group 1",
								ComputeTemplate: "a-template",
								Replicas:        1,
								MinReplicas:     1,
								MaxReplicas:     1,
							},
						},
					},
				},
				Namespace: "a-namespace",
			},
			expectedError: nil,
		},
		{
			name:          "A nil name create service request",
			request:       nil,
			expectedError: util.NewInvalidInputError("A non nill request is expected"),
		},
		{
			name:          "An empty create service request",
			request:       &api.CreateRayServiceRequest{},
			expectedError: util.NewInvalidInputError("Namespace is empty. Please specify a valid value."),
		},
		{
			name: "A create service request with a nill service spec",
			request: &api.CreateRayServiceRequest{
				Namespace: "a-namespace",
				Service:   nil,
			},
			expectedError: util.NewInvalidInputError("Service is empty, please input a valid payload."),
		},
		{
			name: "A create service request with mismatching namespaces",
			request: &api.CreateRayServiceRequest{
				Namespace: "a-namespace",
				Service: &api.RayService{
					Namespace: "another-namespace",
				},
			},
			expectedError: util.NewInvalidInputError("The namespace in the request is different from the namespace in the service definition."),
		},
		{
			name: "A create service request with no name",
			request: &api.CreateRayServiceRequest{
				Namespace: "a-namespace",
				Service: &api.RayService{
					Namespace: "a-namespace",
				},
			},
			expectedError: util.NewInvalidInputError("Service name is empty. Please specify a valid value."),
		},
		{
			name: "A create service request with no user name",
			request: &api.CreateRayServiceRequest{
				Namespace: "a-namespace",
				Service: &api.RayService{
					Namespace: "a-namespace",
					Name:      "fruit-stand",
					User:      "",
				},
			},
			expectedError: util.NewInvalidInputError("User who create the Service is empty. Please specify a valid value."),
		},
		{
			name: "A create service with no service graph or V2 config",
			request: &api.CreateRayServiceRequest{
				Namespace: "a-namespace",
				Service: &api.RayService{
					Namespace: "a-namespace",
					Name:      "fruit-stand",
					User:      "3cp0",
				},
			},
			expectedError: util.NewInvalidInputError("A serve config v2 or deployment graph specs is required. Please specify either."),
		},
		{
			name: "A create service request with both V1 and graph spec",
			request: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Name:           "a-name",
					Namespace:      "a-namespace",
					User:           "a-user",
					ServeConfig_V2: "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: DAGDriver\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n      - name: create_order\n        num_replicas: 1\n      - name: DAGDriver\n        num_replicas: 1\n",
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath: "fruit.deployment_graph",
						RuntimeEnv: "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
						ServeConfigs: []*api.ServeConfig{
							{
								DeploymentName: "OrangeStand",
								Replicas:       1,
								UserConfig:     "price: 2",
								ActorOptions: &api.ActorOptions{
									CpusPerActor: 0.1,
								},
							},
						},
					},
					ServiceUnhealthySecondThreshold:    900,
					DeploymentUnhealthySecondThreshold: 300,
					ClusterSpec: &api.ClusterSpec{
						HeadGroupSpec: &api.HeadGroupSpec{
							ComputeTemplate: "a compute template name",
							RayStartParams: map[string]string{
								"dashboard-host":      "0.0.0.0",
								"metrics-export-port": "8080",
							},
							Volumes: []*api.Volume{},
						},
						WorkerGroupSpec: []*api.WorkerGroupSpec{
							{
								GroupName:       "group 1",
								ComputeTemplate: "a-template",
								Replicas:        1,
								MinReplicas:     1,
								MaxReplicas:     1,
							},
						},
					},
				},
				Namespace: "a-namespace",
			},
			expectedError: util.NewInvalidInputError("Both serve config v2 or deployment graph specs were specified. Please specify one or the other."),
		},
		{
			name: "A create request with no cluster spec",
			request: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Name:                               "a-name",
					Namespace:                          "a-namespace",
					User:                               "a-user",
					ServeConfig_V2:                     "some yaml",
					ServiceUnhealthySecondThreshold:    900,
					DeploymentUnhealthySecondThreshold: 300,
					ClusterSpec:                        nil,
				},
				Namespace: "a-namespace",
			},
			expectedError: util.NewInvalidInputError("A ClusterSpec object is required. Please specify one."),
		},
		{
			name: "A create request with empty deployment graph spec",
			request: &api.CreateRayServiceRequest{
				Service: &api.RayService{
					Name:                     "a-name",
					Namespace:                "a-namespace",
					User:                     "a-user",
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{},
					ClusterSpec:              nil,
				},
				Namespace: "a-namespace",
			},
			expectedError: util.NewInvalidInputError("ServeDeploymentGraphSpec import path must have a value. Please specify valid value."),
		},
		{
			name: "A create request with a invalid deployment graph spec empty serve config",
			request: &api.CreateRayServiceRequest{
				Namespace: "a-namespace",
				Service: &api.RayService{
					Name:      "a-name",
					Namespace: "a-namespace",
					User:      "a-user",
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath: "fruit.deployment_graph",
						ServeConfigs: []*api.ServeConfig{
							{},
						},
					},
				},
			},
			expectedError: util.NewInvalidInputError("ServeConfig 0 deployment name is empty. Please specify a valid value."),
		},
		{
			name: "A create request with a invalid deployment graph spec no replicas serve config",
			request: &api.CreateRayServiceRequest{
				Namespace: "a-namespace",
				Service: &api.RayService{
					Name:      "a-name",
					Namespace: "a-namespace",
					User:      "a-user",
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath: "fruit.deployment_graph",
						ServeConfigs: []*api.ServeConfig{
							{
								DeploymentName: "OrangeStand",
								Replicas:       -1,
							},
						},
					},
				},
			},
			expectedError: util.NewInvalidInputError("ServeConfig 0 replicas must be greater than 0. Please specify a valid value."),
		},
		{
			name: "A create request with a invalid deployment graph spec with invalid actor options",
			request: &api.CreateRayServiceRequest{
				Namespace: "a-namespace",
				Service: &api.RayService{
					Name:      "a-name",
					Namespace: "a-namespace",
					User:      "a-user",
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath: "fruit.deployment_graph",
						ServeConfigs: []*api.ServeConfig{
							{
								DeploymentName: "OrangeStand",
								Replicas:       1,
								ActorOptions: &api.ActorOptions{
									CpusPerActor:   -1,
									GpusPerActor:   -1,
									MemoryPerActor: 0,
								},
							},
						},
					},
				},
			},
			expectedError: util.NewInvalidInputError("ServeConfig 0 invalid ActorOptions, cpusPerActor, gpusPerActor and memoryPerActor must be greater than 0."),
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			actualError := server.ValidateCreateServiceRequest(tc.request)
			if tc.expectedError == nil {
				require.NoError(t, actualError, "No error expected.")
			} else {
				require.EqualError(t, actualError, tc.expectedError.Error(), "A matching error is expected")
			}
		})
	}
}

func TestValidateUpdateRayServiceConfigsRequest(t *testing.T) {
	tests := []struct {
		name          string
		request       *api.UpdateRayServiceConfigsRequest
		expectedError error
	}{
		{
			name: "A valid update request",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:      "a-service-name",
				Namespace: "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{
						{
							GroupName:   "a-group-name",
							Replicas:    1,
							MinReplicas: 1,
							MaxReplicas: 2,
						},
					},
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath: "fruit.deployment_graph",
						RuntimeEnv: "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
						ServeConfigs: []*api.ServeConfig{
							{
								DeploymentName: "OrangeStand",
								Replicas:       1,
								UserConfig:     "price: 2",
								ActorOptions: &api.ActorOptions{
									CpusPerActor: 0.1,
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			name: "A valid update request with only workgroup update spec",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:      "a-service-name",
				Namespace: "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{
						{
							GroupName:   "a group name",
							Replicas:    1,
							MinReplicas: 1,
							MaxReplicas: 2,
						},
					},
					ServeDeploymentGraphSpec: nil,
				},
			},
			expectedError: nil,
		},
		{
			name: "A valid update request with only deployment graph spec",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:      "a-service-name",
				Namespace: "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: nil,
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
						ImportPath: "fruit.deployment_graph",
						RuntimeEnv: "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
						ServeConfigs: []*api.ServeConfig{
							{
								DeploymentName: "OrangeStand",
								Replicas:       1,
								UserConfig:     "price: 2",
								ActorOptions: &api.ActorOptions{
									CpusPerActor: 0.1,
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			name:          "An empty request",
			request:       &api.UpdateRayServiceConfigsRequest{},
			expectedError: util.NewInvalidInputError("Update ray service config request ray service name is empty. Please specify a valid value."),
		},
		{
			name:          "A nil request",
			request:       nil,
			expectedError: util.NewInvalidInputError("Update ray service config request can't be nil."),
		},
		{
			name: "A no namespace request",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:          "a-service-name",
				Namespace:     "",
				UpdateService: &api.UpdateRayServiceBody{},
			},
			expectedError: util.NewInvalidInputError("Update ray service config request ray service namespace is empty. Please specify a valid value."),
		},
		{
			name: "A no service name request",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:          "",
				Namespace:     "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{},
			},
			expectedError: util.NewInvalidInputError("Update ray service config request ray service name is empty. Please specify a valid value."),
		},
		{
			name: "A nil update ray service body",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:          "a-service-name",
				Namespace:     "a-namespace",
				UpdateService: nil,
			},
			expectedError: util.NewInvalidInputError("Update ray service config request spec is empty. Nothing to update."),
		},
		{
			name: "An empty update ray service body",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:          "a-service-name",
				Namespace:     "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{},
			},
			expectedError: util.NewInvalidInputError("Update ray service config request spec is empty. Nothing to update."),
		},
		{
			name: "A worker group spec with no name",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:      "a-service-name",
				Namespace: "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{
						{
							GroupName:   "",
							Replicas:    1,
							MinReplicas: 1,
							MaxReplicas: 2,
						},
					},
				},
			},
			expectedError: util.NewInvalidInputError("Update ray service config request worker group update spec at index %d is missing a name, Please specify a valid value.", 0),
		},
		{
			name: "A worker group spec with invalid replica counts",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:      "a-service-name",
				Namespace: "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{
						{
							GroupName:   "small-wg",
							Replicas:    0,
							MinReplicas: 0,
							MaxReplicas: 0,
						},
					},
				},
			},
			expectedError: util.NewInvalidInputError("Update ray service config request worker group update spec at index %d has invalid values, replicas, minReplicas and maxReplicas must be greater than 0.", 0),
		},
		{
			name: "A worker group spec with invalid min max replica counts",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:      "a-service-name",
				Namespace: "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{
					WorkerGroupUpdateSpec: []*api.WorkerGroupUpdateSpec{
						{
							GroupName:   "small-wg",
							Replicas:    1,
							MinReplicas: 5,
							MaxReplicas: 1,
						},
					},
				},
			},
			expectedError: util.NewInvalidInputError("Update ray service config request worker group update spec with name 'small-wg' has MinReplica > MaxReplicas. Please specify a valid value."),
		},
		{
			name: "An empty ServeDeploymentGraphSpec",
			request: &api.UpdateRayServiceConfigsRequest{
				Name:      "a-service-name",
				Namespace: "a-namespace",
				UpdateService: &api.UpdateRayServiceBody{
					ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{},
				},
			},
			expectedError: util.NewInvalidInputError("ServeDeploymentGraphSpec import path must have a value. Please specify valid value."),
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			actualError := server.ValidateUpdateRayServiceConfigsRequest(tc.request)
			if tc.expectedError == nil {
				require.NoError(t, actualError, "No error expected.")
			} else {
				require.EqualError(t, actualError, tc.expectedError.Error(), "A matching error is expected")
			}
		})
	}
}
