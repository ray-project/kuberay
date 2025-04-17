package session

import (
	"context"
	"fmt"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/completion"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"strings"
)

type KillAllSessionsOptions struct {
	cmdFactory     cmdutil.Factory
	ioStreams      *genericiooptions.IOStreams
	currentContext string
	ResourceType   util.ResourceType
	ResourceName   string
	namespace      string
	Verbose        bool
}

func NewKillAllSessionOptions(cmdFactory cmdutil.Factory, streams genericiooptions.IOStreams) *KillAllSessionsOptions {
	return &KillAllSessionsOptions{
		cmdFactory: cmdFactory,
		ioStreams:  &streams,
	}
}

func NewKillAllSessionsCommand(cmdFactory cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	options := NewKillAllSessionOptions(cmdFactory, streams)

	cmd := &cobra.Command{
		Use:     "kill-all",
		Short:   "Forward local ports to the Ray resources.",
		Long:    sessionLong,
		Example: sessionExample,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return cmdutil.UsageErrorf(cmd, "accepts 0 arg, received %d\n%s", len(args), cmd.Use)
			}
			return nil
		},
		ValidArgsFunction: completion.RayClusterResourceNameCompletionFunc(cmdFactory),
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.KillAll(cmd.Context(), cmdFactory)
		},
	}

	cmd.Flags().BoolVarP(&options.Verbose, "verbose", "v", false, "verbose output")

	return cmd
}

func (options *KillAllSessionsOptions) KillAll(ctx context.Context, factory cmdutil.Factory) error {
	procs, err := process.Processes()
	if err != nil {
		return fmt.Errorf("failed to get processes: %w", err)
	}

	targetCmd := "kubectl-ray session"
	selfCmd := "kubectl-ray session kill-all"

	for _, p := range procs {
		cmdline, err := p.CmdlineWithContext(ctx)
		if err == nil {
			if strings.Contains(cmdline, targetCmd) && !strings.Contains(cmdline, selfCmd) {
				fmt.Printf("Killing process %d: %s\n", p.Pid, cmdline)
				if err := p.Kill(); err != nil {
					return fmt.Errorf("failed to kill process %d: %w", p.Pid, err)
				}
			}
		}
	}

	return nil
}
