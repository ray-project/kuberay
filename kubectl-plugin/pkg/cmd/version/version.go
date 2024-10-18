package version

import (
	"fmt"

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
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Output the version of the Ray kubectl plugin and KubeRay operator",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Println("kubectl ray plugin version:", Version)

			kubeClient, err := client.NewClient(cmdFactory)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			operatorVersion, err := kubeClient.GetKubeRayOperatorVersion(cmd.Context())
			if err != nil {
				fmt.Println("Warning: KubeRay operator installation cannot be found - did you install it with the name \"kuberay-operator\"?")
			} else {
				fmt.Println("KubeRay operator version:", operatorVersion)
			}
			return nil
		},
	}

	return cmd
}
