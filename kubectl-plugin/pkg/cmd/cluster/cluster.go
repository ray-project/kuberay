package cluster

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func NewClusterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "cluster",
		Short:        "Manage ray cluster resources",
		Long:         `Allow users to manage and retrieve ray cluster information and resources.`,
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				fmt.Println(fmt.Errorf("unknown command(s) %q", strings.Join(args, " ")))
			}
			cmd.HelpFunc()(cmd, args)
		},
	}

	cmd.AddCommand(NewClusterGetCommand())
	return cmd
}
