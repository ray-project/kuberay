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
		expectedError error
		clusterSpec   *api.ClusterSpec
		name          string
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
			name: "A head group with A wrong image pull policy",
			clusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: "a template",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
					ImagePullPolicy: "foo",
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{},
			},
			expectedError: util.NewInvalidInputError("HeadGroupSpec unsupported value for Image pull policy. Please specify Always or IfNotPresent"),
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
		expectedError error
		request       *api.CreateRayServiceRequest
		name          string
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
			expectedError: util.NewInvalidInputError("A ClusterSpec object is required. Please specify one."),
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
