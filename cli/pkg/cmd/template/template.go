package template

import (
	"github.com/ray-project/kuberay/cli/pkg/cmd/template/compute"
	"github.com/spf13/cobra"
)

func NewCmdTemplate() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template <command>",
		Short: "Manage templates (compute)",
		Long:  ``,
		Annotations: map[string]string{
			"IsCore": "true",
		},
	}

	cmd.AddCommand(compute.NewCmdComputeTemplate())

	return cmd
}
