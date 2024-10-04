package version

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Version = "development"

func NewVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Output the version of the Ray kubectl plugin",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(Version)
		},
	}
	return cmd
}
