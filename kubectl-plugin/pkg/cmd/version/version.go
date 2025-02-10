package version

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

var Version = "development"

type VersionOptions struct {
	configFlags *genericclioptions.ConfigFlags
	ioStreams   *genericclioptions.IOStreams
}

func NewVersionOptions(streams genericclioptions.IOStreams) *VersionOptions {
	return &VersionOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		ioStreams:   &streams,
	}
}

func NewVersionCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewVersionOptions(streams)
	// Initialize the factory for later use with the current config flag
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:          "version",
		Short:        "Output the version of the Ray kubectl plugin and KubeRay operator",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// running cmd.Execute or cmd.ExecuteE sets the context, which will be done by root
			k8sClient, err := client.NewClient(cmdFactory)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}
			return options.Run(cmd.Context(), k8sClient, os.Stdout)
		},
	}

	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *VersionOptions) Run(ctx context.Context, k8sClient client.Client, writer io.Writer) error {
	fmt.Fprintln(writer, "kubectl ray plugin version:", Version)

	if err := options.checkContext(); err != nil {
		return err
	}

	operatorVersion, err := k8sClient.GetKubeRayOperatorVersion(ctx)
	if err != nil {
		wrappedError := fmt.Errorf(`warning: KubeRay operator installation cannot be found: %w. Did you install it with the name "kuberay-operator"?`, err)
		fmt.Fprintln(writer, wrappedError)
	} else {
		fmt.Fprintln(writer, "KubeRay operator version:", operatorVersion)
	}
	return nil
}

// checkContext checks if a context is set in the kube config or with the --context flag
func (options *VersionOptions) checkContext() error {
	// Overrides and binds the kube config then retrieves the merged result
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("error retrieving raw config: %w", err)
	}

	if !util.HasKubectlContext(config, options.configFlags) {
		return fmt.Errorf("no context is currently set, use %q or %q to select a new one", "--context", "kubectl config use-context <context>")
	}
	return nil
}
