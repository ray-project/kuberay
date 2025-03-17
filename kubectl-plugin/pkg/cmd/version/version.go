package version

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

var Version = "development"

type VersionOptions struct {
	cmdFactory cmdutil.Factory
	ioStreams  *genericclioptions.IOStreams
}

func NewVersionOptions(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *VersionOptions {
	return &VersionOptions{
		cmdFactory: cmdFactory,
		ioStreams:  &streams,
	}
}

func NewVersionCommand(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := NewVersionOptions(cmdFactory, streams)

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

	return cmd
}

func (options *VersionOptions) Run(ctx context.Context, k8sClient client.Client, writer io.Writer) error {
	fmt.Fprintln(writer, "kubectl ray plugin version:", Version)

	operatorVersion, err := k8sClient.GetKubeRayOperatorVersion(ctx)
	if err != nil {
		wrappedError := fmt.Errorf(`warning: KubeRay operator installation cannot be found: %w. Did you install it with the name "kuberay-operator"?`, err)
		fmt.Fprintln(writer, wrappedError)
	} else {
		fmt.Fprintln(writer, "KubeRay operator version:", operatorVersion)
	}
	return nil
}
