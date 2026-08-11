package kuberneteswas

import (
	"errors"
	"fmt"

	"k8s.io/client-go/rest"
)

// selectProvider resolves which versioned provider to use.
//
// For now it auto-detects the first registered provider whose scheduling.k8s.io
// API version is served by the cluster. As the API churns (v1alpha2 -> v1alpha3
// -> ...), additional providers register themselves and this picks the served
// one.
//
// TODO: honor a deploy-time preferred-version list and a per-RayCluster override
// (both must still resolve to a served + registered version).
func selectProvider(config *rest.Config) (Provider, error) {
	if len(registeredProviders) == 0 {
		return nil, fmt.Errorf("no %s providers registered", pluginName)
	}

	// A nil config (e.g. in unit tests) skips discovery and uses the first provider.
	if config == nil {
		return registeredProviders[0], nil
	}

	var unavailable []error
	for _, provider := range registeredProviders {
		if err := provider.Available(config); err != nil {
			unavailable = append(unavailable, err)
			continue
		}
		return provider, nil
	}
	return nil, fmt.Errorf("no served scheduling.k8s.io API version available for %s: %w", pluginName, errors.Join(unavailable...))
}
