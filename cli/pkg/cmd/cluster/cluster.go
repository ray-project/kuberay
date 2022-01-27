package cluster

import (
	"github.com/spf13/cobra"
)

func NewCmdCluster() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster <command>",
		Short: "Manage ray cluster",
		Long:  ``,
		Annotations: map[string]string{
			"IsCore": "true",
		},
	}

	cmd.AddCommand(newCmdGet())
	cmd.AddCommand(newCmdList())
	cmd.AddCommand(newCmdCreate())
	cmd.AddCommand(newCmdDelete())

	return cmd
}
