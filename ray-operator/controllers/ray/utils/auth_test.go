package utils

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupAuthTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = configv1.AddToScheme(s)
	return s
}

func TestDetectAuthenticationMode(t *testing.T) {
	tests := []struct {
		name        string
		expected    AuthenticationMode
		objects     []client.Object
		isOpenShift bool
	}{
		{
			name:        "Not OpenShift - returns ModeIntegratedOAuth (default)",
			isOpenShift: false,
			objects:     []client.Object{},
			expected:    ModeIntegratedOAuth,
		},
		{
			name:        "OpenShift with OIDC providers - returns ModeOIDC",
			isOpenShift: true,
			objects: []client.Object{
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
			expected: ModeOIDC,
		},
		{
			name:        "OpenShift with OAuth identity providers - returns ModeIntegratedOAuth",
			isOpenShift: true,
			objects: []client.Object{
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
			expected: ModeIntegratedOAuth,
		},
		{
			name:        "OpenShift with empty OAuth spec - returns ModeIntegratedOAuth",
			isOpenShift: true,
			objects: []client.Object{
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
			expected: ModeIntegratedOAuth,
		},
		{
			name:        "OpenShift without auth resources - returns ModeIntegratedOAuth (default)",
			isOpenShift: true,
			objects:     []client.Object{},
			expected:    ModeIntegratedOAuth,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup scheme with all required types
			s := setupAuthTestScheme()

			// Create fake client with test objects
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.objects...).
				Build()

			// Test
			result := DetectAuthenticationMode(context.Background(), fakeClient, tc.isOpenShift)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestShouldEnableOAuth(t *testing.T) {
	tests := []struct {
		name                 string
		authMode             AuthenticationMode
		controlledNetworkEnv bool
		expected             bool
	}{
		{
			name:                 "ControlledNetworkEnvironment enabled with IntegratedOAuth - should enable OAuth",
			controlledNetworkEnv: true,
			authMode:             ModeIntegratedOAuth,
			expected:             true,
		},
		{
			name:                 "ControlledNetworkEnvironment enabled with OIDC - should not enable OAuth",
			controlledNetworkEnv: true,
			authMode:             ModeOIDC,
			expected:             false,
		},
		{
			name:                 "ControlledNetworkEnvironment disabled with IntegratedOAuth - should not enable OAuth",
			controlledNetworkEnv: false,
			authMode:             ModeIntegratedOAuth,
			expected:             false,
		},
		{
			name:                 "ControlledNetworkEnvironment disabled with OIDC - should not enable OAuth",
			controlledNetworkEnv: false,
			authMode:             ModeOIDC,
			expected:             false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ShouldEnableOAuth(tc.controlledNetworkEnv, tc.authMode)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestShouldEnableOIDC(t *testing.T) {
	tests := []struct {
		name                 string
		authMode             AuthenticationMode
		controlledNetworkEnv bool
		expected             bool
	}{
		{
			name:                 "ControlledNetworkEnvironment enabled with OIDC - should enable OIDC",
			controlledNetworkEnv: true,
			authMode:             ModeOIDC,
			expected:             true,
		},
		{
			name:                 "ControlledNetworkEnvironment enabled with IntegratedOAuth - should not enable OIDC",
			controlledNetworkEnv: true,
			authMode:             ModeIntegratedOAuth,
			expected:             false,
		},
		{
			name:                 "ControlledNetworkEnvironment disabled with OIDC - should not enable OIDC",
			controlledNetworkEnv: false,
			authMode:             ModeOIDC,
			expected:             false,
		},
		{
			name:                 "ControlledNetworkEnvironment disabled with IntegratedOAuth - should not enable OIDC",
			controlledNetworkEnv: false,
			authMode:             ModeIntegratedOAuth,
			expected:             false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ShouldEnableOIDC(tc.controlledNetworkEnv, tc.authMode)
			assert.Equal(t, tc.expected, result)
		})
	}
}
