package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/cobra"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

const (
	RaySessionCommand        = "kubectl-ray session"
	RaySessionKillAllCommand = "kubectl-ray session kill-all"
)

var (
	sessionKillAllLong = templates.LongDesc(`
		Kill all Ray session processes started by kubectl-ray session command.
	`)

	sessionKillAllExample = templates.Examples(`
		kubectl ray session kill-all
	`)
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
		Short:   "Kill all Ray sessions",
		Long:    sessionKillAllLong,
		Example: sessionKillAllExample,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return cmdutil.UsageErrorf(cmd, "accepts 0 arg, received %d\n%s", len(args), cmd.Use)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
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

	for _, p := range procs {
		cmdline, err := p.CmdlineWithContext(ctx)
		if err != nil {
			// Skip the process if we can't get its command line. It might be permission issue.
			continue
		}
		if strings.Contains(cmdline, RaySessionCommand) && !strings.Contains(cmdline, RaySessionKillAllCommand) {
			if options.Verbose {
				fmt.Printf("Found ray session: %s\n", cmdline)
			}
			// Since ray session spawn child processes to run the actual commands,
			// we need to kill all child processes first.
			children, err := p.ChildrenWithContext(ctx)
			if err != nil {
				return fmt.Errorf("failed to get children of process %d: %w", p.Pid, err)
			}
			for _, child := range children {
				if options.Verbose {
					fmt.Printf("Killing subprocess with PID %d\n", child.Pid)
				}
				if err := child.Kill(); err != nil {
					return fmt.Errorf("failed to kill child process %d: %w", child.Pid, err)
				}
			}
			// Then kill the parent process.
			if options.Verbose {
				fmt.Printf("Killing process with PID %d\n", p.Pid)
			}
			if err := p.Kill(); err != nil {
				return fmt.Errorf("failed to kill process %d: %w", p.Pid, err)
			}
		}
	}
	return nil
}
