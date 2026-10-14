// Command release-test validates a KubeRay release candidate on a kind cluster. An LLM only plans
// units and triages failures; this program does the rest. See README.md.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ray-project/kuberay/release-test/pkg/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := cmd.NewReleaseTestCommand().ExecuteContext(ctx)
	stop()
	if err != nil {
		os.Exit(1)
	}
}
