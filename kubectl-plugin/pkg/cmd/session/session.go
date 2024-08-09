package session

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/cmd/portforward"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"github.com/spf13/cobra"
)

const (
	DASHBOARD_PORT = 8265
	CLIENT_PORT    = 10001
)

type SessionOptions struct {
	ioStreams    *genericiooptions.IOStreams
	configFlags  *genericclioptions.ConfigFlags
	ResourceName string
	Namespace    string
}

func NewSessionOptions(streams genericiooptions.IOStreams) *SessionOptions {
	return &SessionOptions{
		ioStreams:   &streams,
		configFlags: genericclioptions.NewConfigFlags(true),
	}
}

func NewSessionCommand(streams genericiooptions.IOStreams) *cobra.Command {
	options := NewSessionOptions(streams)
	factory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:   "session NAME",
		Short: "Forward local ports to the Ray resources. Currently only supports RayCluster.",
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
	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *SessionOptions) Complete(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, "%s", cmd.Use)
	}
	options.ResourceName = args[0]

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
		return fmt.Errorf("Error retrieving raw config: %w", err)
	}
	if len(config.CurrentContext) == 0 {
		return fmt.Errorf("no context is currently set, use %q to select a new one", "kubectl config use-context <context>")
	}
	return nil
}

func (options *SessionOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	kubeClientSet, err := factory.KubernetesClientSet()
	if err != nil {
		return fmt.Errorf("failed to initialize clientset: %w", err)
	}

	svcName, err := findServiceName(ctx, kubeClientSet, options.Namespace, options.ResourceName)
	if err != nil {
		return err
	}

	portForwardCmd := portforward.NewCmdPortForward(factory, *options.ioStreams)
	portForwardCmd.SetArgs([]string{svcName, fmt.Sprintf("%d:%d", DASHBOARD_PORT, DASHBOARD_PORT), fmt.Sprintf("%d:%d", CLIENT_PORT, CLIENT_PORT)})

	fmt.Printf("Ray Dashboard: http://localhost:%d\nRay Interactive Client: http://localhost:%d\n\n", DASHBOARD_PORT, CLIENT_PORT)

	if err := portForwardCmd.ExecuteContext(ctx); err != nil {
		return fmt.Errorf("failed to port-forward: %w", err)
	}

	return nil
}

func findServiceName(ctx context.Context, kubeClientSet kubernetes.Interface, namespace, resourceName string) (string, error) {
	listopts := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("ray.io/cluster=%s, ray.io/node-type=head", resourceName),
	}

	rayHeadSvcs, err := kubeClientSet.CoreV1().Services(namespace).List(ctx, listopts)
	if err != nil {
		return "", fmt.Errorf("unable to retrieve ray head services: %w", err)
	}

	if len(rayHeadSvcs.Items) == 0 {
		return "", fmt.Errorf("no ray head services found")
	}
	if len(rayHeadSvcs.Items) > 1 {
		return "", fmt.Errorf("more than one ray head service found")
	}

	rayHeadSrc := rayHeadSvcs.Items[0]
	return "service/" + rayHeadSrc.Name, nil
}
