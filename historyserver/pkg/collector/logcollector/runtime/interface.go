package runtime

import (
	"context"
)

type RayLogCollector interface {
	Run(stop <-chan struct{}) error
}

// OwnerResolver is an interface for resolving the owner of a RayCluster
// returns (ownerKind, ownerName, error)
type OwnerResolver interface {
	Resolve(ctx context.Context, namespace, rayClusterName string) (string, string, error)
}
