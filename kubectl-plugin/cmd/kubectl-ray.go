package main

import (
	"os"

	cmd "github.com/ray-project/kuberay/kubectl-plugin/pkg/cmd"
	flag "github.com/spf13/pflag"
)

func main() {
	flags := flag.NewFlagSet("kubectl-ray", flag.ExitOnError)
	flag.CommandLine = flags

	root := cmd.NewRayCommand()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
