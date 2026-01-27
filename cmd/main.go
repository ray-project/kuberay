package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/ray-project/kuberay/podpool/manager"
)

var (
	kubeconfig    string
	nodeName      string
	operatingSys  string
	arch          string
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig file")
	flag.StringVar(&nodeName, "node-name", "cache-pod-node", "Name of the virtual node")
	flag.StringVar(&operatingSys, "os", "linux", "Operating system of the virtual node")
	flag.StringVar(&arch, "arch", "amd64", "Architecture of the virtual node")
}

func main() {
	// Create a Cobra command to handle command line arguments
	cmd := &cobra.Command{
		Use:   "cache-pod-manager",
		Short: "Virtual Kubelet-based Cache Pod Manager",
		Long:  "A Virtual Kubelet implementation that manages cached pods",
		Run: func(cmd *cobra.Command, args []string) {
			run(cmd.Context())
		},
	}

	// Execute the command
	if err := cmd.Execute(); err != nil {
		klog.Fatalf("Command execution failed: %v", err)
	}
}

func run(ctx context.Context) {
	// Load the Kubernetes configuration
	var config *clientcmd.ClientConfig
	if kubeconfig != "" {
		// Use the provided kubeconfig file
		configOverride := &clientcmd.ConfigOverrides{}
		config = &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}.ClientConfig()
	} else {
		// Use in-cluster config if available, otherwise fall back to default config
		config = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&configOverride,
		)
	}

	restConfig, err := config.ClientConfig()
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client config: %v", err)
	}

	// Create the Kubernetes client
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Create the CachePodManager
	cachePodManager, err := manager.NewCachePodManager(nodeName, operatingSys, arch, clientset)
	if err != nil {
		klog.Fatalf("Failed to create CachePodManager: %v", err)
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the CachePodManager
	if err := cachePodManager.Start(ctx); err != nil {
		klog.Fatalf("Failed to start CachePodManager: %v", err)
	}

	// Set up the pod notifier
	cachePodManager.NotifyPods(ctx, func(pod *corev1.Pod) {
		// Handle pod notifications
		klog.Infof("Received pod notification for %s/%s", pod.Namespace, pod.Name)
	})

	// Wait for shutdown signal
	select {
	case <-sigCh:
		klog.Info("Received shutdown signal")
	case <-ctx.Done():
		klog.Info("Context cancelled")
	}

	cancel()
}