package config

import (
	"fmt"

	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdGet() *cobra.Command {
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get configuration in kuberay with key.",
		Long:  `Get configuration in kuberay. Use argument as the key`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			// key, _ := cmd.Flags().GetString("key")
			val := cmdutil.GetVal(key)
			fmt.Printf("%s\n", val)
		},
	}
	return getCmd
}
