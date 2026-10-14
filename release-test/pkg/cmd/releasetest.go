package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/ray-project/kuberay/release-test/pkg/cmd/lint"
	"github.com/ray-project/kuberay/release-test/pkg/cmd/run"
	"github.com/ray-project/kuberay/release-test/pkg/cmd/stale"
)

var releaseTestLong = templates.LongDesc(`
	Validate a KubeRay release candidate against release-test/items.yaml on a local kind cluster.

	An LLM only plans units (the release-test-plan skill) and triages failures (the release-test-run
	skill); this program does the rest. Run it from the kuberay repository. See release-test/README.md.
`)

func NewReleaseTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "release-test",
		Short:        "Validate a KubeRay release candidate",
		Long:         releaseTestLong,
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	cmd.AddCommand(stale.NewStaleCommand())
	cmd.AddCommand(lint.NewLintCommand())
	cmd.AddCommand(run.NewRunCommand())

	return cmd
}
