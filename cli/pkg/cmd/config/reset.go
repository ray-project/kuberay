package config

import (
	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdReset() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration in kuberay to default.",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.Reset()
		},
	}
	return cmd
}
