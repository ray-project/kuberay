package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"k8s.io/klog/v2"

	"github.com/ray-project/kuberay/experimental/pkg/grpcproxy"
	"github.com/ray-project/kuberay/experimental/pkg/httpproxy"
)

func main() {
	// Ports
	http_remote_port := os.Getenv("HTTP_REMOTE_PORT")
	http_local_port := os.Getenv("HTTP_LOCAL_PORT")
	grpc_remote_port := os.Getenv("GRPC_REMOTE_PORT")
	grpc_local_port := os.Getenv("GRPC_LOCAL_PORT")
	// Uncomment below for local testing
	/*	http_remote_port := "9091"
		http_local_port := "9090"
		grpc_remote_port := "9091"
		grpc_local_port := "9090" */

	// Security info
	secure_prefix := os.Getenv("SECURITY_PREFIX")
	security_token := os.Getenv("SECURITY_TOKEN")
	// Uncomment below for local testing
	/*	secure_prefix := "/" // Only used for HTTP to define secure prefix
		security_token := "12345"*/

	// Enabling GRPC
	enable_grpc := strings.ToLower(os.Getenv("ENABLE_GRPC")) == "true"
	// Uncomment below for local testing
	//	enable_grpc := true

	if http_remote_port == "" || http_local_port == "" || secure_prefix == "" || security_token == "" {
		klog.Fatal("Failing to get execution parameters - http remote port: ", http_remote_port, " http local port: ", http_local_port, " security prefix: ", secure_prefix)
	}
	if enable_grpc && (grpc_remote_port == "" || grpc_local_port == "") {
		klog.Fatal("Failing to get execution parameters - grpc remote port: ", grpc_remote_port, " grpc local port: ", grpc_local_port)
	}
	klog.Info("Starting reverse proxy with parameters - http remote port: ", http_remote_port, " http local port: ", http_local_port, " security prefix: ", secure_prefix)
	if enable_grpc {
		klog.Info(" grpc remote port: ", grpc_remote_port, " grpc local port: ", grpc_local_port)
	}

	// GRPC proxy
	if enable_grpc {
		go func() {
			remote_url := "http://localhost:" + http_remote_port
			// Client connection
			cc, err := grpc.NewClient(remote_url, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				klog.Fatal("cannot dial server: ", err)
			}
			defer cc.Close()
			klog.Info("connecting to GRPC server ", cc.Target())

			// set up listener
			lis, err := net.Listen("tcp", "localhost:"+grpc_local_port)
			if err != nil {
				klog.Fatal("failed to listen: ", err)
			}
			klog.Info("GRPC listening on ", lis.Addr())

			// Director function
			directorFn := func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				outCtx := metadata.NewOutgoingContext(ctx, md.Copy())
				return outCtx, cc, nil
			}

			// Set up the proxy server and then serve from it.
			sh, h := grpcproxy.TransparentHandler(directorFn)
			proxySrv := grpc.NewServer(grpc.UnknownServiceHandler(sh))
			// Add security header checking
			h.AddSecurityHeaderToHandler(map[string]string{"Authorization": security_token})
			klog.Info("Security header added ")

			// run GRPC proxy backend
			klog.Info("Running GRPC proxy")
			if err := proxySrv.Serve(lis); err != nil {
				klog.Fatal("server stopped unexpectedly")
			}
		}()
	}

	// HTTP proxy. Start it as a goroutine so that we can start GRPC one as well
	remote_url := "http://localhost:" + http_remote_port
	remote, err := url.Parse(remote_url)
	if err != nil {
		klog.Fatal("Failed to parse remote url ", remote_url, " - error: ", err)
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(remote)
	klog.Info("Connected to remote HTTP ", remote_url)
	// Create token authorization
	token := httpproxy.NewTokenAuth(security_token, proxy, secure_prefix, remote)
	// Create handler
	http.HandleFunc("/", token.AuthFunc())
	// Run HTTP proxy
	err = http.ListenAndServe(":"+http_local_port, nil)
	if err != nil {
		klog.Fatal("HTTP server died unexpectedly, error - ", err)
	}
}
