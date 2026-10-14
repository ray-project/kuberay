package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerdict(t *testing.T) {
	dir := t.TempDir()
	// reply writes the JSON that `claude -p --output-format json` prints, with result as its reply.
	reply := func(result string) string {
		path := filepath.Join(dir, "triage.json")
		data, err := json.Marshal(map[string]any{"type": "result", "result": result})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o600))
		return path
	}

	triage, brief, err := readVerdict(reply("Triage complete.\n\nx doc-bug: \"kustomization pins namespace: default\""), "x")
	require.NoError(t, err)
	assert.Equal(t, "doc-bug", triage)
	assert.Equal(t, "kustomization pins namespace: default", brief, "a quoted brief with a colon")

	triage, brief, err = readVerdict(reply("x plan-bug: wrong label\nOn second thought:\n`x env: kind has 8 CPUs`\n"), "x")
	require.NoError(t, err)
	assert.Equal(t, "env", triage, "the last verdict counts")
	assert.Equal(t, "kind has 8 CPUs", brief)

	_, _, err = readVerdict(reply("y doc-bug: another unit\nx bogus: no such class"), "x")
	require.ErrorContains(t, err, "no verdict line")
}
