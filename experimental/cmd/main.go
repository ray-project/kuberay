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
	httpRemotePort := os.Getenv("HTTP_REMOTE_PORT")
	httpLocalPort := os.Getenv("HTTP_LOCAL_PORT")
	grpcRemotePort := os.Getenv("GRPC_REMOTE_PORT")
	grpcLocalPort := os.Getenv("GRPC_LOCAL_PORT")
	// Uncomment below for local testing
	/*	httpRemotePort := "9091"
		httpLocalPort := "9090"
		grpcRemotePort := "9091"
		grpcLocalPort := "9090" */

	// Security info
	securePrefix := os.Getenv("SECURITY_PREFIX")
	securityToken := os.Getenv("SECURITY_TOKEN")
	// Uncomment below for local testing
	/*	securePrefix := "/" // Only used for HTTP to define secure prefix
		securityToken := "12345"*/

	// Enabling GRPC
	enableGrpc := strings.ToLower(os.Getenv("ENABLE_GRPC")) == "true"
	// Uncomment below for local testing
	//	enableGrpc := true

	if httpRemotePort == "" || httpLocalPort == "" || securePrefix == "" || securityToken == "" {
		klog.Fatal("Failing to get execution parameters - http remote port: ", httpRemotePort, " http local port: ", httpLocalPort, " security prefix: ", securePrefix)
	}
	if enableGrpc && (grpcRemotePort == "" || grpcLocalPort == "") {
		klog.Fatal("Failing to get execution parameters - grpc remote port: ", grpcRemotePort, " grpc local port: ", grpcLocalPort)
	}
	klog.Info("Starting reverse proxy with parameters - http remote port: ", httpRemotePort, " http local port: ", httpLocalPort, " security prefix: ", securePrefix)
	if enableGrpc {
		klog.Info(" grpc remote port: ", grpcRemotePort, " grpc local port: ", grpcLocalPort)
	}

	// GRPC proxy
	if enableGrpc {
		go func() {
			remoteURL := "http://localhost:" + httpRemotePort
			// Client connection
			cc, err := grpc.NewClient(remoteURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				klog.Fatal("cannot dial server: ", err)
			}
			defer cc.Close()
			klog.Info("connecting to GRPC server ", cc.Target())

			// set up listener
			lc := net.ListenConfig{}
			lis, err := lc.Listen(context.Background(), "tcp", "localhost:"+grpcLocalPort)
			if err != nil {
				klog.Fatal("failed to listen: ", err)
			}
			klog.Info("GRPC listening on ", lis.Addr())

			// Director function
			directorFn := func(ctx context.Context, _ string) (context.Context, *grpc.ClientConn, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				outCtx := metadata.NewOutgoingContext(ctx, md.Copy())
				return outCtx, cc, nil
			}

			// Set up the proxy server and then serve from it.
			sh, h := grpcproxy.TransparentHandler(directorFn)
			proxySrv := grpc.NewServer(grpc.UnknownServiceHandler(sh))
			// Add security header checking
			h.AddSecurityHeaderToHandler(map[string]string{"Authorization": securityToken})
			klog.Info("Security header added ")

			// run GRPC proxy backend
			klog.Info("Running GRPC proxy")
			if err := proxySrv.Serve(lis); err != nil {
				klog.Fatal("server stopped unexpectedly")
			}
		}()
	}

	// HTTP proxy. Start it as a goroutine so that we can start GRPC one as well
	remoteURL := "http://localhost:" + httpRemotePort
	remote, err := url.Parse(remoteURL)
	if err != nil {
		klog.Fatal("Failed to parse remote url ", remoteURL, " - error: ", err)
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(remote)
	klog.Info("Connected to remote HTTP ", remoteURL)
	// Create token authorization
	token := httpproxy.NewTokenAuth(securityToken, proxy, securePrefix, remote)
	// Create handler
	http.HandleFunc("/", token.AuthFunc())
	// Run HTTP proxy with timeouts
	server := &http.Server{
		Addr:         ":" + httpLocalPort,
		ReadTimeout:  0, // No timeout
		WriteTimeout: 0, // No timeout
		IdleTimeout:  0, // No timeout
	}
	err = server.ListenAndServe()
	if err != nil {
		klog.Fatal("HTTP server died unexpectedly, error - ", err)
	}
}
