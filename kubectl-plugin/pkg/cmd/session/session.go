package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/completion"
)

type appPort struct {
	name string
	port int
}

type SessionOptions struct {
	cmdFactory     cmdutil.Factory
	ioStreams      *genericiooptions.IOStreams
	currentContext string
	ResourceType   util.ResourceType
	ResourceName   string
	namespace      string
	Verbose        bool
	killAll        bool
}

const (
	reconnectDelay    = 3 * time.Second
	raySessionCommand = "kubectl-ray session"
)

var (
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

        # Kill all existing Ray sessions started by kubectl-ray session command
		kubectl ray session --kill-all
	`)
)

func NewSessionOptions(cmdFactory cmdutil.Factory, streams genericiooptions.IOStreams) *SessionOptions {
	return &SessionOptions{
		cmdFactory: cmdFactory,
		ioStreams:  &streams,
	}
}

func NewSessionCommand(cmdFactory cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	options := NewSessionOptions(cmdFactory, streams)

	cmd := &cobra.Command{
		Use:     "session (RAYCLUSTER | TYPE/NAME)",
		Short:   "Forward local ports to the Ray resources.",
		Long:    sessionLong,
		Example: sessionExample,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return cmdutil.UsageErrorf(cmd, "accepts at most 1 arg, received %d\n%s", len(args), cmd.Use)
			}
			return nil
		},
		ValidArgsFunction: completion.RayClusterResourceNameCompletionFunc(cmdFactory),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(cmd, args); err != nil {
				return err
			}
			return options.Run(cmd.Context(), cmdFactory)
		},
	}

	cmd.Flags().BoolVarP(&options.Verbose, "verbose", "v", false, "verbose output")
	cmd.Flags().BoolVarP(&options.killAll, "kill-all", "", false, "kill all existing Ray sessions started by kubectl-ray session command")
	return cmd
}

func (options *SessionOptions) Complete(cmd *cobra.Command, args []string) error {
	if options.killAll {
		if len(args) > 0 {
			return cmdutil.UsageErrorf(cmd, "accepts no args when killAll flag is set")
		}
		return nil
	}

	if len(args) == 0 {
		return cmdutil.UsageErrorf(cmd, "killAll flag is not set, but no args provided")
	}

	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}
	options.namespace = namespace
	if options.namespace == "" {
		options.namespace = "default"
	}

	context, err := cmd.Flags().GetString("context")
	if err != nil || context == "" {
		config, err := options.cmdFactory.ToRawKubeConfigLoader().RawConfig()
		if err != nil {
			return fmt.Errorf("error retrieving raw config: %w", err)
		}
		context = config.CurrentContext
	}
	options.currentContext = context

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

	return nil
}

func (options *SessionOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	if options.killAll {
		if options.Verbose {
			fmt.Println("killAll flag is set, killing all existing Ray sessions...")
		}
		if err := killAllRaySessions(ctx, options.Verbose); err != nil {
			return fmt.Errorf("failed to kill all ray sessions: %w", err)
		}
		return nil
	}

	k8sClient, err := client.NewClient(factory)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	svcName, err := k8sClient.GetRayHeadSvcName(ctx, options.namespace, options.ResourceType, options.ResourceName)
	if err != nil {
		return err
	}
	fmt.Printf("Forwarding ports to service %s\n", svcName)

	var appPorts []appPort
	switch options.ResourceType {
	case util.RayCluster:
		appPorts = []appPort{dashboardPort, clientPort}
	case util.RayJob:
		appPorts = []appPort{dashboardPort}
	case util.RayService:
		appPorts = []appPort{dashboardPort, servePort}
	default:
		return fmt.Errorf("unsupported resource type: %s", options.ResourceType)
	}

	var kubectlArgs []string
	if options.currentContext != "" {
		kubectlArgs = append(kubectlArgs, "--context", options.currentContext)
	}
	// TODO (dxia): forward more global kubectl flags like --server, --insecure-skip-tls-verify, --token, --tls-server-name, --user, --password, etc?

	kubectlArgs = append(kubectlArgs, "port-forward", "-n", options.namespace, "service/"+svcName)
	for _, appPort := range appPorts {
		kubectlArgs = append(kubectlArgs, fmt.Sprintf("%d:%d", appPort.port, appPort.port))
	}

	for _, appPort := range appPorts {
		fmt.Printf("%s: http://localhost:%d\n", appPort.name, appPort.port)
	}
	fmt.Println()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			portforwardCmd := exec.Command("kubectl", kubectlArgs...)
			portforwardCmd.Stdout = options.ioStreams.Out
			portforwardCmd.Stderr = options.ioStreams.ErrOut

			if options.Verbose {
				fmt.Printf("Running: %s\n", strings.Join(portforwardCmd.Args, " "))
			}

			if err = portforwardCmd.Run(); err == nil {
				return
			}
			fmt.Printf("failed to port-forward: %v. Retrying in %v ...\n\n", err, reconnectDelay)
			time.Sleep(reconnectDelay)
		}
	}()

	wg.Wait()
	return nil
}

func killAllRaySessions(ctx context.Context, verbose bool) error {
	procs, err := process.Processes()
	if err != nil {
		return fmt.Errorf("failed to get processes: %w", err)
	}

	for _, p := range procs {
		cmdline, err := p.CmdlineWithContext(ctx)
		if err != nil {
			// Skip the process if we can't get its command line. It might be permission issue.
			continue
		}
		if strings.Contains(cmdline, raySessionCommand) && int(p.Pid) != os.Getpid() {
			if verbose {
				fmt.Printf("Found ray session: %s\n", cmdline)
			}
			// Since ray session spawn child processes to run the actual commands,
			// we need to kill all child processes first.
			children, err := p.ChildrenWithContext(ctx)
			if err != nil {
				return fmt.Errorf("failed to get children of process %d: %w", p.Pid, err)
			}
			for _, child := range children {
				if verbose {
					fmt.Printf("Killing subprocess with PID %d\n", child.Pid)
				}
				if err := child.Kill(); err != nil {
					return fmt.Errorf("failed to kill child process %d: %w", child.Pid, err)
				}
			}
			// Then kill the parent process.
			if verbose {
				fmt.Printf("Killing process with PID %d\n", p.Pid)
			}
			if err := p.Kill(); err != nil {
				return fmt.Errorf("failed to kill process %d: %w", p.Pid, err)
			}
		}
	}
	return nil
}
