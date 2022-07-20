package config

import (
	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdSet() *cobra.Command {
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set configuration in kuberay.",
		Long:  `Set configuration in kuberay. Use the first argument as key and the second argument as value`,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			value := args[1]
			// key, _ := cmd.Flags().GetString("key")
			// value, _ := cmd.Flags().GetString("value")
			cmdutil.SetKeyValPair(key, value)
		},
	}
	return setCmd
}
