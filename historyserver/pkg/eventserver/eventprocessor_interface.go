package eventserver

import "context"

type EventProcessor[T any] interface {
	ProcessEvents(ctx context.Context, ch <-chan T) error
}
