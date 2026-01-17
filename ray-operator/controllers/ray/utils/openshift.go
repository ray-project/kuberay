package utils

import (
	"os"
	"strings"

	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
)

// IsOpenShiftCluster detects if the cluster is OpenShift by checking for OpenShift-specific API groups.
// This function is called once at operator startup and the result is stored in reconciler options.
func IsOpenShiftCluster() bool {
	// Check for OpenShift API groups
	config, err := ctrl.GetConfig()
	if err != nil || config == nil {
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
		for _, openshiftGroup := range openshiftGroups {
			if strings.Contains(group.Name, openshiftGroup) {
				return true
			}
		}
	}

	return false
}

// ShouldUseIngressOnOpenShift determines if Ingress should be used instead of Route on OpenShift
func ShouldUseIngressOnOpenShift() bool {
	return os.Getenv(USE_INGRESS_ON_OPENSHIFT) == "true"
}
