package config

import "github.com/spf13/cobra"

func NewCmdConfig() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config <command>",
		Short: "Kuberay Config Management",
		Long:  ``,
		Annotations: map[string]string{
			"IsCore": "false",
		},
	}

	configCmd.AddCommand(NewCmdSet())
	configCmd.AddCommand(NewCmdReset())
	configCmd.AddCommand(NewCmdGet())

	return configCmd
}
