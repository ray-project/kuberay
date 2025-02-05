package scale

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func NewScaleCommand(streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "scale",
		Short:        "Scale a Ray resource",
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				fmt.Println(fmt.Errorf("unknown command(s) %q", strings.Join(args, " ")))
			}
			cmd.HelpFunc()(cmd, args)
		},
	}

	cmd.AddCommand(NewScaleClusterCommand(streams))
	return cmd
}
