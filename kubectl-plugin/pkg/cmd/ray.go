package cmd

import (
	cluster "github.com/ray-project/kuberay/kubectl-plugin/pkg/cmd/cluster"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

func NewRayCommand(streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ray",
		Short:        "ray kubectl plugin",
		Long:         "Manage RayCluster resources.",
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
	}

	cmd.AddCommand(cluster.NewClusterCommand(streams))
	return cmd
}
