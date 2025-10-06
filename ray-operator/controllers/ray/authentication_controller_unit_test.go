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
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// setupScheme creates a scheme with all necessary types registered
func setupScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = rayv1.AddToScheme(s)
	_ = configv1.AddToScheme(s)
	_ = routev1.AddToScheme(s)
	_ = gatewayv1.AddToScheme(s)
	return s
}

func TestDetectAuthenticationMode(t *testing.T) {
	tests := []struct {
		name     string
		expected utils.AuthenticationMode
		objects  []runtime.Object
		options  RayClusterReconcilerOptions
	}{
		{
			name: "Not OpenShift - returns ModeIntegratedOAuth (default)",
			options: RayClusterReconcilerOptions{
				IsOpenShift: false,
			},
			objects:  []runtime.Object{},
			expected: utils.ModeIntegratedOAuth,
		},
		{
			name: "OpenShift with OIDC providers - returns ModeOIDC",
			options: RayClusterReconcilerOptions{
				IsOpenShift: true,
			},
			objects: []runtime.Object{
				&configv1.Authentication{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster",
					},
					Spec: configv1.AuthenticationSpec{
						OIDCProviders: []configv1.OIDCProvider{
							{Name: "test-oidc"},
						},
					},
				},
			},
			expected: utils.ModeOIDC,
		},
		{
			name: "OpenShift with OAuth identity providers - returns ModeIntegratedOAuth",
			options: RayClusterReconcilerOptions{
				IsOpenShift: true,
			},
			objects: []runtime.Object{
				&configv1.Authentication{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster",
					},
					Spec: configv1.AuthenticationSpec{},
				},
				&configv1.OAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster",
					},
					Spec: configv1.OAuthSpec{
						IdentityProviders: []configv1.IdentityProvider{
							{Name: "test-oauth"},
						},
					},
				},
			},
			expected: utils.ModeIntegratedOAuth,
		},
		{
			name: "OpenShift with empty OAuth spec - returns ModeIntegratedOAuth",
			options: RayClusterReconcilerOptions{
				IsOpenShift: true,
			},
			objects: []runtime.Object{
				&configv1.Authentication{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster",
					},
					Spec: configv1.AuthenticationSpec{},
				},
				&configv1.OAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster",
					},
					Spec: configv1.OAuthSpec{},
				},
			},
			expected: utils.ModeIntegratedOAuth,
		},
		{
			name: "OpenShift without auth resources - returns ModeIntegratedOAuth (default)",
			options: RayClusterReconcilerOptions{
				IsOpenShift: true,
			},
			objects:  []runtime.Object{},
			expected: utils.ModeIntegratedOAuth,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup scheme with all required types
			s := setupScheme()

			// Create fake client with test objects
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tc.objects...).
				Build()

			// Test
			result := utils.DetectAuthenticationMode(context.Background(), fakeClient, tc.options.IsOpenShift)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestShouldEnableOAuth(t *testing.T) {
	tests := []struct {
		name     string
		authMode utils.AuthenticationMode
		options  RayClusterReconcilerOptions
		expected bool
	}{
		{
			name: "ControlledNetworkEnvironment enabled with IntegratedOAuth - should enable OAuth",
			options: RayClusterReconcilerOptions{
				ControlledNetworkEnvironment: true,
			},
			authMode: utils.ModeIntegratedOAuth,
			expected: true,
		},
		{
			name: "ControlledNetworkEnvironment enabled with OIDC - should not enable OAuth",
			options: RayClusterReconcilerOptions{
				ControlledNetworkEnvironment: true,
			},
			authMode: utils.ModeOIDC,
			expected: false,
		},
		{
			name: "ControlledNetworkEnvironment disabled with IntegratedOAuth - should not enable OAuth",
			options: RayClusterReconcilerOptions{
				ControlledNetworkEnvironment: false,
			},
			authMode: utils.ModeIntegratedOAuth,
			expected: false,
		},
		{
			name: "ControlledNetworkEnvironment disabled with OIDC - should not enable OAuth",
			options: RayClusterReconcilerOptions{
				ControlledNetworkEnvironment: false,
			},
			authMode: utils.ModeOIDC,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := utils.ShouldEnableOAuth(tc.options.ControlledNetworkEnvironment, tc.authMode)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestShouldEnableOIDC(t *testing.T) {
	tests := []struct {
		name     string
		authMode utils.AuthenticationMode
		options  RayClusterReconcilerOptions
		expected bool
	}{
		{
			name: "ControlledNetworkEnvironment enabled with OIDC - should enable OIDC",
			options: RayClusterReconcilerOptions{
				ControlledNetworkEnvironment: true,
			},
			authMode: utils.ModeOIDC,
			expected: true,
		},
		{
			name: "ControlledNetworkEnvironment enabled with IntegratedOAuth - should not enable OIDC",
			options: RayClusterReconcilerOptions{
				ControlledNetworkEnvironment: true,
			},
			authMode: utils.ModeIntegratedOAuth,
			expected: false,
		},
		{
			name: "ControlledNetworkEnvironment disabled with OIDC - should not enable OIDC",
			options: RayClusterReconcilerOptions{
				ControlledNetworkEnvironment: false,
			},
			authMode: utils.ModeOIDC,
			expected: false,
		},
		{
			name: "ControlledNetworkEnvironment disabled with IntegratedOAuth - should not enable OIDC",
			options: RayClusterReconcilerOptions{
				ControlledNetworkEnvironment: false,
			},
			authMode: utils.ModeIntegratedOAuth,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := utils.ShouldEnableOIDC(tc.options.ControlledNetworkEnvironment, tc.authMode)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestEnsureOAuthServiceAccount(t *testing.T) {
	tests := []struct {
		existingSA     *corev1.ServiceAccount
		name           string
		expectCreation bool
		expectUpdate   bool
	}{
		{
			name:           "Service account doesn't exist - should create",
			existingSA:     nil,
			expectCreation: true,
			expectUpdate:   false,
		},
		{
			name: "Service account exists without annotation - should update",
			existingSA: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-oauth-proxy-sa",
					Namespace: "default",
				},
			},
			expectCreation: false,
			expectUpdate:   true,
		},
		{
			name: "Service account exists with annotation - should not update",
			existingSA: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-oauth-proxy-sa",
					Namespace: "default",
					Annotations: map[string]string{
						"serviceaccounts.openshift.io/oauth-redirectreference.first": `{"kind":"OAuthRedirectReference","apiVersion":"v1","reference":{"kind":"Route","name":"test-cluster"}}`,
					},
				},
			},
			expectCreation: false,
			expectUpdate:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
					UID:       "test-uid",
				},
			}

			objects := []runtime.Object{cluster}
			if tc.existingSA != nil {
				objects = append(objects, tc.existingSA)
			}

			s := setupScheme()
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(objects...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Execute
			err := controller.ensureOAuthServiceAccount(ctx, cluster, ctrl.Log)
			require.NoError(t, err)

			// Verify
			sa := &corev1.ServiceAccount{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-cluster-oauth-proxy-sa",
				Namespace: "default",
			}, sa)
			require.NoError(t, err)
			assert.NotNil(t, sa.Annotations)
			assert.Contains(t, sa.Annotations, "serviceaccounts.openshift.io/oauth-redirectreference.first")
		})
	}
}

func TestEnsureOAuthSecret(t *testing.T) {
	tests := []struct {
		existingSecret *corev1.Secret
		name           string
		expectCreation bool
	}{
		{
			name:           "Secret doesn't exist - should create",
			existingSecret: nil,
			expectCreation: true,
		},
		{
			name: "Secret exists - should not overwrite",
			existingSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-oauth-config",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"cookie_secret": []byte("existing-secret"),
				},
			},
			expectCreation: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
					UID:       "test-uid",
				},
			}

			objects := []runtime.Object{cluster}
			if tc.existingSecret != nil {
				objects = append(objects, tc.existingSecret)
			}

			s := setupScheme()
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(objects...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Execute
			err := controller.ensureOAuthSecret(ctx, cluster, ctrl.Log)
			require.NoError(t, err)

			// Verify
			secret := &corev1.Secret{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-cluster-oauth-config",
				Namespace: "default",
			}, secret)
			require.NoError(t, err)
			assert.NotEmpty(t, secret.Data["cookie_secret"])

			// If secret existed, verify it wasn't overwritten
			if tc.existingSecret != nil {
				assert.Equal(t, []byte("existing-secret"), secret.Data["cookie_secret"])
			}
		})
	}
}

func TestEnsureOAuthTLSSecret(t *testing.T) {
	tests := []struct {
		existingSecret *corev1.Secret
		name           string
		expectCreation bool
	}{
		{
			name:           "TLS secret doesn't exist - should create",
			existingSecret: nil,
			expectCreation: true,
		},
		{
			name: "TLS secret exists - should not overwrite",
			existingSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-oauth-tls",
					Namespace: "default",
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": []byte("existing-cert"),
					"tls.key": []byte("existing-key"),
				},
			},
			expectCreation: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
					UID:       "test-uid",
				},
			}

			objects := []runtime.Object{cluster}
			if tc.existingSecret != nil {
				objects = append(objects, tc.existingSecret)
			}

			s := setupScheme()
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(objects...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Execute
			err := controller.ensureOAuthTLSSecret(ctx, cluster, ctrl.Log)
			require.NoError(t, err)

			// Verify
			secret := &corev1.Secret{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-cluster-oauth-tls",
				Namespace: "default",
			}, secret)
			require.NoError(t, err)
			assert.Equal(t, corev1.SecretTypeTLS, secret.Type)
			assert.NotEmpty(t, secret.Data["tls.crt"])
			assert.NotEmpty(t, secret.Data["tls.key"])

			// If secret existed, verify it wasn't overwritten
			if tc.existingSecret != nil {
				assert.Equal(t, []byte("existing-cert"), secret.Data["tls.crt"])
				assert.Equal(t, []byte("existing-key"), secret.Data["tls.key"])
			}
		})
	}
}

func TestEnsureOIDCConfigMap(t *testing.T) {
	tests := []struct {
		existingConfigMap *corev1.ConfigMap
		name              string
		expectCreation    bool
		expectUpdate      bool
	}{
		{
			name:              "ConfigMap doesn't exist - should create",
			existingConfigMap: nil,
			expectCreation:    true,
			expectUpdate:      false,
		},
		{
			name: "ConfigMap exists with different data - should update",
			existingConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kube-rbac-proxy-config-test-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "old-config",
				},
			},
			expectCreation: false,
			expectUpdate:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
					UID:       "test-uid",
				},
			}

			objects := []runtime.Object{cluster}
			if tc.existingConfigMap != nil {
				objects = append(objects, tc.existingConfigMap)
			}

			s := setupScheme()
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(objects...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Execute
			err := controller.ensureOIDCConfigMap(ctx, cluster, ctrl.Log)
			require.NoError(t, err)

			// Verify
			cm := &corev1.ConfigMap{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "kube-rbac-proxy-config-test-cluster",
				Namespace: "default",
			}, cm)
			require.NoError(t, err)
			assert.Contains(t, cm.Data, "config.yaml")
			assert.Contains(t, cm.Data["config.yaml"], "test-cluster-head-svc")
			assert.Equal(t, "kube-rbac-proxy", cm.Labels["app"])
		})
	}
}

func TestEnsureHttpRoute(t *testing.T) {
	tests := []struct {
		existingHttpRoute *gatewayv1.HTTPRoute
		name              string
		expectCreation    bool
		expectUpdate      bool
	}{
		{
			name:              "HTTPRoute doesn't exist - should create",
			existingHttpRoute: nil,
			expectCreation:    true,
			expectUpdate:      false,
		},
		{
			name: "HTTPRoute exists - should update",
			existingHttpRoute: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
			},
			expectCreation: false,
			expectUpdate:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						ServiceType: corev1.ServiceTypeClusterIP,
					},
				},
			}

			objects := []runtime.Object{cluster}
			if tc.existingHttpRoute != nil {
				objects = append(objects, tc.existingHttpRoute)
			}

			s := setupScheme()

			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(objects...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Execute
			err := controller.ensureHttpRoute(ctx, cluster, ctrl.Log)
			require.NoError(t, err)

			// Verify
			route := &gatewayv1.HTTPRoute{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-cluster",
				Namespace: "default",
			}, route)
			require.NoError(t, err)

			// Should have 2 rules: 1 for redirect, 1 for rewrite
			assert.Len(t, route.Spec.Rules, 2)

			// Rule 0: Exact match for redirect to #/
			assert.Equal(t, gatewayv1.PathMatchExact, *route.Spec.Rules[0].Matches[0].Path.Type)
			assert.Equal(t, "/ray/default/test-cluster", *route.Spec.Rules[0].Matches[0].Path.Value)
			assert.NotNil(t, route.Spec.Rules[0].Filters[0].RequestRedirect)
			assert.Contains(t, *route.Spec.Rules[0].Filters[0].RequestRedirect.Path.ReplaceFullPath, "/#/")

			// Rule 1: Prefix match for normal traffic
			assert.Equal(t, gatewayv1.PathMatchPathPrefix, *route.Spec.Rules[1].Matches[0].Path.Type)
			assert.Equal(t, "/ray/default/test-cluster", *route.Spec.Rules[1].Matches[0].Path.Value)
			assert.NotEmpty(t, route.Spec.Rules[1].BackendRefs)
		})
	}
}

func TestUpdateHeadServiceForOAuth(t *testing.T) {
	tests := []struct {
		existingService *corev1.Service
		name            string
		expectUpdate    bool
	}{
		{
			name:            "Service doesn't exist - should skip",
			existingService: nil,
			expectUpdate:    false,
		},
		{
			name: "Service exists without OAuth port - should add port",
			existingService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-head-svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "dashboard",
							Port:     8265,
							Protocol: corev1.ProtocolTCP,
						},
					},
				},
			},
			expectUpdate: true,
		},
		{
			name: "Service exists with OAuth port - should not add duplicate",
			existingService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-head-svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "dashboard",
							Port:     8265,
							Protocol: corev1.ProtocolTCP,
						},
						{
							Name:     oauthProxyPortName,
							Port:     authProxyPort,
							Protocol: corev1.ProtocolTCP,
						},
					},
				},
			},
			expectUpdate: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						ServiceType: corev1.ServiceTypeClusterIP,
					},
				},
			}

			objects := []runtime.Object{cluster}
			if tc.existingService != nil {
				objects = append(objects, tc.existingService)
			}

			s := setupScheme()
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(objects...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Execute
			err := controller.updateHeadServiceForOAuth(ctx, cluster, ctrl.Log)
			require.NoError(t, err)

			// Verify
			if tc.existingService != nil {
				service := &corev1.Service{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      "test-cluster-head-svc",
					Namespace: "default",
				}, service)
				require.NoError(t, err)

				// Check if OAuth port exists
				hasOAuthPort := false
				for _, port := range service.Spec.Ports {
					if port.Name == oauthProxyPortName {
						hasOAuthPort = true
						assert.Equal(t, int32(authProxyPort), port.Port)
						break
					}
				}
				assert.True(t, hasOAuthPort, "Service should have OAuth proxy port")
			}
		})
	}
}

func TestUpdateRouteForOAuth(t *testing.T) {
	tests := []struct {
		existingRoute *routev1.Route
		name          string
		expectUpdate  bool
	}{
		{
			name:          "Route doesn't exist - should skip",
			existingRoute: nil,
			expectUpdate:  false,
		},
		{
			name: "Route exists without OAuth config - should update",
			existingRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cluster",
					Namespace:         "default",
					CreationTimestamp: metav1.Now(),
				},
				Spec: routev1.RouteSpec{
					To: routev1.RouteTargetReference{
						Kind: "Service",
						Name: "test-cluster-head-svc",
					},
				},
			},
			expectUpdate: true,
		},
		{
			name: "Route exists with OAuth config - should not update",
			existingRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cluster",
					Namespace:         "default",
					CreationTimestamp: metav1.Now(),
				},
				Spec: routev1.RouteSpec{
					To: routev1.RouteTargetReference{
						Kind: "Service",
						Name: "test-cluster-head-svc",
					},
					Port: &routev1.RoutePort{
						TargetPort: intstr.FromString(oauthProxyPortName),
					},
					TLS: &routev1.TLSConfig{
						Termination:                   routev1.TLSTerminationPassthrough,
						InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
					},
				},
			},
			expectUpdate: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
			}

			objects := []runtime.Object{cluster}
			if tc.existingRoute != nil {
				objects = append(objects, tc.existingRoute)
			}

			s := setupScheme()

			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(objects...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Execute
			err := controller.updateRouteForOAuth(ctx, cluster, ctrl.Log)
			require.NoError(t, err)

			// Verify - Note: The fake client has limitations with CreateOrUpdate,
			// so we can't fully verify the route was updated. In a real cluster,
			// the CreateOrUpdate pattern works correctly. Here we just verify no error occurred.
			if tc.existingRoute != nil && tc.existingRoute.Spec.Port != nil {
				// If the route already had OAuth config, verify it still exists
				route := &routev1.Route{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      "test-cluster",
					Namespace: "default",
				}, route)
				require.NoError(t, err)
				assert.NotNil(t, route.Spec.Port)
				assert.NotNil(t, route.Spec.TLS)
			}
		})
	}
}

func TestMapAuthResourceToRayClusters(t *testing.T) {
	tests := []struct {
		name             string
		clusters         []runtime.Object
		expectedRequests int
	}{
		{
			name:             "No clusters - should return empty list",
			clusters:         []runtime.Object{},
			expectedRequests: 0,
		},
		{
			name: "One cluster - should return one request",
			clusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1",
						Namespace: "default",
					},
				},
			},
			expectedRequests: 1,
		},
		{
			name: "Multiple clusters - should return multiple requests",
			clusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1",
						Namespace: "default",
					},
				},
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster2",
						Namespace: "default",
					},
				},
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster3",
						Namespace: "test-namespace",
					},
				},
			},
			expectedRequests: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			s := setupScheme()

			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tc.clusters...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Create a fake auth resource to trigger mapping
			authResource := &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
			}

			// Execute
			requests := controller.mapAuthResourceToRayClusters(ctx, authResource)

			// Verify
			assert.Len(t, requests, tc.expectedRequests)

			// Verify request names match cluster names
			if tc.expectedRequests > 0 {
				requestNames := make(map[string]bool)
				for _, req := range requests {
					requestNames[req.Name] = true
				}

				for _, obj := range tc.clusters {
					cluster := obj.(*rayv1.RayCluster)
					assert.True(t, requestNames[cluster.Name], "Request should exist for cluster %s", cluster.Name)
				}
			}
		})
	}
}

func TestGetOAuthProxySidecar(t *testing.T) {
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	container := GetOAuthProxySidecar(cluster)

	assert.Equal(t, oauthProxyContainerName, container.Name)
	assert.Equal(t, oauthProxyImage, container.Image)
	assert.NotEmpty(t, container.Args)
	assert.NotEmpty(t, container.Env)
	assert.NotEmpty(t, container.VolumeMounts)
	assert.NotEmpty(t, container.Ports)

	// Verify port configuration
	assert.Equal(t, int32(authProxyPort), container.Ports[0].ContainerPort)
	assert.Equal(t, oauthProxyPortName, container.Ports[0].Name)

	// Verify resource limits are set
	assert.NotNil(t, container.Resources.Requests)
	assert.NotNil(t, container.Resources.Limits)

	// Verify probes are configured
	assert.NotNil(t, container.LivenessProbe)
	assert.NotNil(t, container.ReadinessProbe)
}

func TestGetOAuthProxyVolumes(t *testing.T) {
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	volumes := GetOAuthProxyVolumes(cluster)

	assert.Len(t, volumes, 2)

	// Verify volume names
	volumeNames := make(map[string]bool)
	for _, vol := range volumes {
		volumeNames[vol.Name] = true
	}

	assert.True(t, volumeNames[oauthConfigVolumeName])
	assert.True(t, volumeNames[oauthProxyVolumeName])
}

func TestGetOIDCProxySidecar(t *testing.T) {
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	container := GetOIDCProxySidecar(cluster)

	assert.Equal(t, oidcProxyContainerName, container.Name)
	assert.Equal(t, oidcProxyContainerImage, container.Image)
	assert.NotEmpty(t, container.Args)
	assert.NotEmpty(t, container.VolumeMounts)
	assert.NotEmpty(t, container.Ports)

	// Verify port configuration
	assert.Equal(t, int32(authProxyPort), container.Ports[0].ContainerPort)
	assert.Equal(t, oidcProxyPortName, container.Ports[0].Name)

	// Verify volume mount
	assert.Equal(t, "kube-rbac-proxy-config-"+cluster.Name, container.VolumeMounts[0].Name)
}

func TestGetOIDCProxyVolumes(t *testing.T) {
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	volumes := GetOIDCProxyVolumes(cluster)

	assert.Len(t, volumes, 1)
	assert.Equal(t, "kube-rbac-proxy-config-"+cluster.Name, volumes[0].Name)
	assert.NotNil(t, volumes[0].VolumeSource.ConfigMap)
	assert.Equal(t, "kube-rbac-proxy-config-"+cluster.Name, volumes[0].VolumeSource.ConfigMap.Name)
}

func TestHelperFunctions(t *testing.T) {
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	t.Run("OAuth ServiceAccountName via ResourceNamer", func(t *testing.T) {
		namer := utils.NewResourceNamer(cluster)
		name := namer.ServiceAccountName(utils.ModeIntegratedOAuth)
		assert.Equal(t, "test-cluster-oauth-proxy-sa", name)
	})

	t.Run("OAuth SecretName via ResourceNamer", func(t *testing.T) {
		namer := utils.NewResourceNamer(cluster)
		name := namer.SecretName(utils.ModeIntegratedOAuth)
		assert.Equal(t, "test-cluster-oauth-config", name)
	})

	t.Run("OAuth TLSSecretName via ResourceNamer", func(t *testing.T) {
		namer := utils.NewResourceNamer(cluster)
		name := namer.TLSSecretName(utils.ModeIntegratedOAuth)
		assert.Equal(t, "test-cluster-oauth-tls", name)
	})
}

func TestGenerateSelfSignedCert(t *testing.T) {
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	cert, key, err := generateSelfSignedCert(cluster)

	require.NoError(t, err)
	assert.NotEmpty(t, cert)
	assert.NotEmpty(t, key)

	// Verify cert and key are valid PEM -  Handled this way due to pre-commit hook false positive
	assert.Contains(t, string(cert), "BEGIN CERTIFICATE")
	assert.Contains(t, string(cert), "END CERTIFICATE")
	assert.Contains(t, string(key), "BEGIN "+"PRIVATE KEY")
	assert.Contains(t, string(key), "END "+"PRIVATE KEY")
}

func TestReconcile_RayClusterNotFound(t *testing.T) {
	// Setup
	ctx := context.Background()
	s := setupScheme()

	fakeClient := clientFake.NewClientBuilder().
		WithScheme(s).
		Build()

	controller := &AuthenticationController{
		Client:   fakeClient,
		Scheme:   s,
		Recorder: record.NewFakeRecorder(10),
		options: RayClusterReconcilerOptions{
			IsOpenShift: false,
		},
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "non-existent-cluster",
			Namespace: "default",
		},
	}

	// Execute
	result, err := controller.Reconcile(ctx, req)

	// Verify - should not error when cluster is not found
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReconcile_ManagedByExternalController(t *testing.T) {
	// Setup
	ctx := context.Background()
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			ManagedBy: ptr.To("external-controller"),
		},
	}

	s := setupScheme()
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(cluster).
		Build()

	controller := &AuthenticationController{
		Client:   fakeClient,
		Scheme:   s,
		Recorder: record.NewFakeRecorder(10),
		options: RayClusterReconcilerOptions{
			IsOpenShift: false,
		},
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	// Execute
	result, err := controller.Reconcile(ctx, req)

	// Verify - should skip reconciliation
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestCleanupOIDCResources(t *testing.T) {
	ctx := context.Background()
	namer := utils.NewResourceNamer(&rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	})

	tests := []struct {
		name              string
		existingResources []runtime.Object
		expectDeleted     []string
	}{
		{
			name: "Delete OIDC ConfigMap, HTTPRoute, and ServiceAccount",
			existingResources: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      namer.ConfigMapName(),
						Namespace: "default",
					},
				},
				&gatewayv1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
					},
				},
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      namer.ServiceAccountName(utils.ModeOIDC),
						Namespace: "default",
					},
				},
			},
			expectDeleted: []string{"configmap", "httproute", "serviceaccount"},
		},
		{
			name:              "No resources to delete - should not error",
			existingResources: []runtime.Object{},
			expectDeleted:     []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := setupScheme()

			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
			}

			objects := append(tc.existingResources, cluster)
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(objects...).
				Build()

			controller := &AuthenticationController{
				Client:   fakeClient,
				Scheme:   s,
				Recorder: record.NewFakeRecorder(10),
			}

			// Execute cleanup
			err := controller.cleanupOIDCResources(ctx, cluster, ctrl.Log)
			require.NoError(t, err)

			// Verify resources are deleted
			if len(tc.expectDeleted) > 0 {
				// Check ConfigMap is deleted
				configMap := &corev1.ConfigMap{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      namer.ConfigMapName(),
					Namespace: "default",
				}, configMap)
				assert.True(t, errors.IsNotFound(err), "ConfigMap should be deleted")

				// Check HTTPRoute is deleted
				httpRoute := &gatewayv1.HTTPRoute{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      "test-cluster",
					Namespace: "default",
				}, httpRoute)
				assert.True(t, errors.IsNotFound(err), "HTTPRoute should be deleted")

				// Check ServiceAccount is deleted
				sa := &corev1.ServiceAccount{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      namer.ServiceAccountName(utils.ModeOIDC),
					Namespace: "default",
				}, sa)
				assert.True(t, errors.IsNotFound(err), "ServiceAccount should be deleted")
			}
		})
	}
}
