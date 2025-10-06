package utils

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AuthenticationMode represents the type of authentication configured in the cluster
type AuthenticationMode string

const (
	// ModeIntegratedOAuth represents OpenShift's integrated OAuth authentication
	ModeIntegratedOAuth AuthenticationMode = "IntegratedOAuth"
	// ModeOIDC represents OIDC-based authentication
	ModeOIDC AuthenticationMode = "OIDC"
)

// DetectAuthenticationMode determines whether the cluster is using OAuth or OIDC
// Returns IntegratedOAuth by default when no specific authentication is configured
func DetectAuthenticationMode(ctx context.Context, k8sClient client.Client, isOpenShift bool) AuthenticationMode {
	// First, check if we're on OpenShift
	if !isOpenShift {
		// Default to IntegratedOAuth for non-OpenShift clusters
		return ModeIntegratedOAuth
	}

	// Check for OIDC configuration in the Authentication resource
	auth := &configv1.Authentication{}
	oidcConfigured := false
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "cluster"}, auth); err == nil {
		// Check if OIDC providers are configured
		if len(auth.Spec.OIDCProviders) > 0 {
			return ModeOIDC
		}
		oidcConfigured = true // Successfully retrieved auth resource, OIDC not configured
	}

	// Check for Integrated OAuth configuration in the OAuth resource
	oauth := &configv1.OAuth{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "cluster"}, oauth); err == nil {
		// Check if OAuth identity providers are configured
		if len(oauth.Spec.IdentityProviders) > 0 {
			return ModeIntegratedOAuth
		}
		// If OAuth spec exists but is empty/null, and OIDC is not configured,
		// assume OAuth is the default authentication method on OpenShift
		if oidcConfigured {
			return ModeIntegratedOAuth
		}
	}

	// Default to IntegratedOAuth when no specific authentication is configured
	return ModeIntegratedOAuth
}

// ShouldEnableOAuth determines if OAuth should be enabled based on ControlledNetworkEnvironment and authentication mode
// Returns true only if both ControlledNetworkEnvironment is enabled AND the authentication mode is IntegratedOAuth
func ShouldEnableOAuth(controlledNetworkEnv bool, authMode AuthenticationMode) bool {
	return controlledNetworkEnv && authMode == ModeIntegratedOAuth
}

// ShouldEnableOIDC determines if OIDC should be enabled based on ControlledNetworkEnvironment and authentication mode
// Returns true only if both ControlledNetworkEnvironment is enabled AND the authentication mode is OIDC
func ShouldEnableOIDC(controlledNetworkEnv bool, authMode AuthenticationMode) bool {
	return controlledNetworkEnv && authMode == ModeOIDC
}
