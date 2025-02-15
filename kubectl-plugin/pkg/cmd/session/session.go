package session

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/completion"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	cmdportforward "k8s.io/kubectl/pkg/cmd/portforward"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

type appPort struct {
	name string
	port int
}

type SessionOptions struct {
	configFlags    *genericclioptions.ConfigFlags
	ioStreams      *genericiooptions.IOStreams
	currentContext string
	ResourceType   util.ResourceType
	ResourceName   string
	Namespace      string
	Verbose        bool
}

type defaultPortForwarder struct {
	genericiooptions.IOStreams
}

var (
	gcsPort = appPort{
		name: "Ray GCS Server",
		port: 6379,
	}
	dashboardPort = appPort{
		name: "Ray Dashboard",
		port: 8265,
	}
	clientPort = appPort{
		name: "Ray Interactive Client",
		port: 10001,
	}
	servePort = appPort{
		name: "Ray Serve",
		port: 8000,
	}
)

var (
	sessionLong = templates.LongDesc(`
		Forward local ports to the Ray resources.

		Forward different local ports depending on the resource type: RayCluster, RayJob, or RayService.
	`)

	sessionExample = templates.Examples(`
		# Without specifying the resource type, forward local ports to the Ray cluster
		kubectl ray session my-raycluster

		# Forward local ports to the Ray cluster
		kubectl ray session raycluster/my-raycluster

		# Forward local ports to the Ray cluster used for the Ray job
		kubectl ray session rayjob/my-rayjob

		# Forward local ports to the Ray cluster used for the RayService resource
		kubectl ray session rayservice/my-rayservice
	`)
)

func NewSessionOptions(streams genericiooptions.IOStreams) *SessionOptions {
	configFlags := genericclioptions.NewConfigFlags(true)
	return &SessionOptions{
		ioStreams:   &streams,
		configFlags: configFlags,
	}
}

func NewSessionCommand(streams genericiooptions.IOStreams) *cobra.Command {
	options := NewSessionOptions(streams)
	factory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:               "session (RAYCLUSTER | TYPE/NAME)",
		Short:             "Forward local ports to the Ray resources.",
		Long:              sessionLong,
		Example:           sessionExample,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completion.RayClusterResourceNameCompletionFunc(factory),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(cmd, args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			return options.Run(cmd.Context(), factory)
		},
	}

	cmd.Flags().BoolVarP(&options.Verbose, "verbose", "v", false, "verbose output")

	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *SessionOptions) Complete(cmd *cobra.Command, args []string) error {
	typeAndName := strings.Split(args[0], "/")
	if len(typeAndName) == 1 {
		options.ResourceType = util.RayCluster
		options.ResourceName = typeAndName[0]
	} else {
		if len(typeAndName) != 2 || typeAndName[1] == "" {
			return cmdutil.UsageErrorf(cmd, "invalid resource type/name: %s", args[0])
		}

		switch typeAndName[0] {
		case string(util.RayCluster):
			options.ResourceType = util.RayCluster
		case string(util.RayJob):
			options.ResourceType = util.RayJob
		case string(util.RayService):
			options.ResourceType = util.RayService
		default:
			return cmdutil.UsageErrorf(cmd, "unsupported resource type: %s", typeAndName[0])
		}

		options.ResourceName = typeAndName[1]
	}

	if *options.configFlags.Namespace == "" {
		options.Namespace = "default"
	} else {
		options.Namespace = *options.configFlags.Namespace
	}

	return nil
}

func (options *SessionOptions) Validate() error {
	// Overrides and binds the kube config then retrieves the merged result
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("error retrieving raw config: %w", err)
	}
	if !util.HasKubectlContext(config, options.configFlags) {
		return fmt.Errorf("no context is currently set, use %q or %q to select a new one", "--context", "kubectl config use-context <context>")
	}
	options.currentContext = config.CurrentContext
	return nil
}

func (options *SessionOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	fmt.Printf("Forwarding ports to %s %s in namespace %s\n", options.ResourceType, options.ResourceName, options.Namespace)

	k8sClient, err := client.NewClient(factory)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	podName, err := k8sClient.GetRayHeadPodName(ctx, options.Namespace, options.ResourceType, options.ResourceName)
	if err != nil {
		return err
	}

	if options.Verbose {
		fmt.Printf("using Pod %s\n", podName)
	}

	var appPorts []appPort
	switch options.ResourceType {
	case util.RayCluster:
		appPorts = []appPort{gcsPort, dashboardPort, clientPort}
	case util.RayJob:
		appPorts = []appPort{dashboardPort}
	case util.RayService:
		appPorts = []appPort{dashboardPort, servePort}
	default:
		return fmt.Errorf("unsupported resource type: %s", options.ResourceType)
	}

	var ports []string
	for _, appPort := range appPorts {
		ports = append(ports, fmt.Sprintf("%d", appPort.port))
	}

	config, err := factory.ToRESTConfig()
	if err != nil {
		return err
	}

	pfo := cmdportforward.PortForwardOptions{
		Namespace:  options.Namespace,
		PodName:    podName,
		RESTClient: k8sClient.KubernetesClient().CoreV1().RESTClient(),
		Config:     config,
		PodClient:  k8sClient.KubernetesClient().CoreV1(),
		Address:    []string{"localhost"},
		Ports:      ports,
		PortForwarder: &defaultPortForwarder{
			IOStreams: *options.ioStreams,
		},
	}

	err = pfo.Validate()
	if err != nil {
		return err
	}

	return pfo.RunPortForwardContext(ctx)
}

// copied from https://github.com/kubernetes/kubectl/blob/v0.31.1/pkg/cmd/portforward/portforward.go#L158-L168
func (f *defaultPortForwarder) ForwardPorts(method string, url *url.URL, opts cmdportforward.PortForwardOptions) error {
	dialer, err := createDialer(method, url, opts)
	if err != nil {
		return err
	}
	fw, err := portforward.NewOnAddresses(dialer, opts.Address, opts.Ports, opts.StopChannel, opts.ReadyChannel, f.Out, f.ErrOut)
	if err != nil {
		return err
	}
	return fw.ForwardPorts()
}

// copied from https://github.com/kubernetes/kubectl/blob/v0.31.1/pkg/cmd/portforward/portforward.go#L139-L156
func createDialer(method string, url *url.URL, opts cmdportforward.PortForwardOptions) (httpstream.Dialer, error) {
	transport, upgrader, err := spdy.RoundTripperFor(opts.Config)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, method, url)
	if !cmdutil.PortForwardWebsockets.IsDisabled() {
		tunnelingDialer, err := portforward.NewSPDYOverWebsocketDialer(url, opts.Config)
		if err != nil {
			return nil, err
		}
		// First attempt tunneling (websocket) dialer, then fallback to spdy dialer.
		dialer = portforward.NewFallbackDialer(tunnelingDialer, dialer, func(err error) bool {
			return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
		})
	}
	return dialer, nil
}
