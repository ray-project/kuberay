package utils

import (
	"os"
	"slices"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// IsOpenShiftCluster detects if the cluster is OpenShift by checking for OpenShift-specific API groups.
// This function is called once at operator startup and the result is stored in reconciler options.
func IsOpenShiftCluster(config *rest.Config) bool {
	// Check for OpenShift API groups
	if config == nil {
		return false
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil || discoveryClient == nil {
		return false
	}

	apiGroups, err := discoveryClient.ServerGroups()
	if err != nil {
		return false
	}

	// Check for multiple OpenShift-specific API groups
	openshiftGroups := []string{
		"route.openshift.io",
		"security.openshift.io",
		"config.openshift.io",
	}

	for _, group := range apiGroups.Groups {
		if slices.Contains(openshiftGroups, group.Name) {
			return true
		}
	}

	return false
}

// ShouldUseIngressOnOpenShift determines if Ingress should be used instead of Route on OpenShift
func ShouldUseIngressOnOpenShift() bool {
	return os.Getenv(USE_INGRESS_ON_OPENSHIFT) == "true"
}
