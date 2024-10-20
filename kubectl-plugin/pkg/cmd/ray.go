package cmd

import (
	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/spf13/cobra"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/cmd/cluster"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/cmd/job"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/cmd/log"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/cmd/session"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/cmd/version"
)

func NewRayCommand(streams genericiooptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ray",
		Short:        "ray kubectl plugin",
		Long:         "Manage RayCluster resources.",
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	cmd.AddCommand(cluster.NewClusterCommand(streams))
	cmd.AddCommand(session.NewSessionCommand(streams))
	cmd.AddCommand(log.NewClusterLogCommand(streams))
	cmd.AddCommand(job.NewJobCommand(streams))
	cmd.AddCommand(version.NewVersionCommand(streams))

	return cmd
}
