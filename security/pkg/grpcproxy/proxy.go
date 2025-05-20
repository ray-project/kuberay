package grpcproxy

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// NewProxy sets up a simple proxy that forwards all requests to dst.
func NewProxy(dst *grpc.ClientConn, opts ...grpc.ServerOption) (*grpc.Server, *handler) {
	option, h := DefaultProxyOpt(dst)
	opts = append(opts, option)
	// Set up the proxy server and then serve from it like in step one.
	return grpc.NewServer(opts...), h
}

// DefaultProxyOpt returns an grpc.UnknownServiceHandler with a DefaultDirector.
func DefaultProxyOpt(cc *grpc.ClientConn) (grpc.ServerOption, *handler) {
	sh, h := TransparentHandler(DefaultDirector(cc))
	return grpc.UnknownServiceHandler(sh), h
}

// DefaultDirector returns a very simple forwarding StreamDirector that forwards all
// calls.
func DefaultDirector(cc *grpc.ClientConn) StreamDirector {
	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		ctx = metadata.NewOutgoingContext(ctx, md.Copy())
		return ctx, cc, nil
	}
}
