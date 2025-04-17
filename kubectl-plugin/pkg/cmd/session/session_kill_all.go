package session

import (
	"context"
	"fmt"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/cobra"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"strings"
)

type KillAllSessionsOptions struct {
	Verbose bool
}

func NewKillAllSessionOptions() *KillAllSessionsOptions {
	return &KillAllSessionsOptions{}
}

func NewKillAllSessionsCommand() *cobra.Command {
	options := NewKillAllSessionOptions()

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
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.KillAll(cmd.Context())
		},
	}

	cmd.Flags().BoolVarP(&options.Verbose, "verbose", "v", false, "verbose output")
	return cmd
}

func (options *KillAllSessionsOptions) KillAll(ctx context.Context) error {
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
