package interceptor

import (
	"context"

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
