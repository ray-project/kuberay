package create

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func NewCreateCommand(streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create Ray resources",
		Long:  `Allow users to create Ray resources. And based on input, will generate the necessary files`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				fmt.Println(fmt.Errorf("unknown command(s) %q", strings.Join(args, " ")))
			}
			cmd.HelpFunc()(cmd, args)
		},
	}

	cmd.AddCommand(NewCreateClusterCommand(streams))
	cmd.AddCommand(NewCreateWorkerGroupCommand(streams))
	return cmd
}
