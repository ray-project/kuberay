package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommands(t *testing.T) {
	// Every command checks the tag before using it; main prints nothing, so cobra must print the error.
	want := `--kuberay-version "../../etc" is not a release tag`
	for _, command := range []string{"run", "stale"} {
		t.Run(command, func(t *testing.T) {
			cmd := NewReleaseTestCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{command, "--kuberay-version", "../../etc"})
			require.ErrorContains(t, cmd.ExecuteContext(context.Background()), want)
			assert.Contains(t, out.String(), "Error: "+want)
		})
	}
}
