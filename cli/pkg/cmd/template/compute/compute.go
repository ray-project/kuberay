package compute

import "github.com/spf13/cobra"

func NewCmdComputeTemplate() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compute <command>",
		Short: "Manage compute template",
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
