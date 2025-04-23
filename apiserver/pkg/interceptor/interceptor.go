package interceptor

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	klog "k8s.io/klog/v2"
)

// APIServerInterceptor implements UnaryServerInterceptor that provides the common wrapping logic
// to be executed before and after all API handler calls, e.g. Logging, error handling.
// For more details, see https://github.com/grpc/grpc-go/blob/master/interceptor.go
func APIServerInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	klog.Infof("%v handler starting", info.FullMethod)
	resp, err = handler(ctx, req)
	if err != nil {
		klog.Warning(err)
	}
	klog.Infof("%v handler finished", info.FullMethod)
	return
}

// TimeoutInterceptor implements UnaryServerInterceptor that sets the timeout for the request
func TimeoutInterceptor(timeout time.Duration) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Create a context with timeout
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Channel to capture execution result
		done := make(chan struct{})
		var (
			resp interface{}
			err  error
		)

		go func() {
			resp, err = handler(ctx, req)
			close(done)
		}()

		select {
		case <-ctx.Done():
			// Raise error if time out
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("grpc server timed out")
			}
			return nil, ctx.Err()
		case <-done:
			// Handler finished
			return resp, err
		}
	}
}
