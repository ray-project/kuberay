package main

import (
	"os"

	flag "github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/cmd"
)

func main() {
	flags := flag.NewFlagSet("kubectl-ray", flag.ExitOnError)
	flag.CommandLine = flags
	ioStreams := genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}

	// Cancel the command context on SIGINT/SIGTERM so commands that spawn child
	// processes can tear them down. A second signal exits immediately.
	ctx := ctrl.SetupSignalHandler()

	root := cmd.NewRayCommand(ioStreams)
	if err := root.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
