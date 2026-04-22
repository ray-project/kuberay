package eventserver

import (
	"context"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

// EventProcessor builds a SessionSnapshot for a single Ray cluster session
// by reading and merging raw events from object storage on demand.
type EventProcessor interface {
	// Process is expected to be safe to call concurrently. Concurrent calls
	// with the same sessionID are deduplicated upstream by SessionResolver
	// via singleflight, so implementations do not need session-level locking.
	Process(ctx context.Context, sessionID string) (*types.SessionSnapshot, error)
}
