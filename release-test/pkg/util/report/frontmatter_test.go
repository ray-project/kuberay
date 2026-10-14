package report

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The skill must not touch the front matter; when it does, the runner's replaces it and the body stays.
func TestWriteFrontMatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	broken := "---\nitem: x\nstatus: fail\nbrief: page says: use -n ray-system\n---\n\n# x\n\n| table |\n\n## Triage\n\nThe cause.\n"
	require.NoError(t, os.WriteFile(path, []byte(broken), 0o600))
	_, err := ReadFrontMatter(path)
	require.Error(t, err, "the skill wrote an unquoted colon into brief")

	two := 2
	metadata := FrontMatter{Item: "x", Status: "fail", Version: "v9.9.9", FailedStep: &two, Triage: "doc-bug", Brief: "page says: use -n ray-system"}
	require.NoError(t, WriteFrontMatter(path, metadata))
	got, err := ReadFrontMatter(path)
	require.NoError(t, err)
	assert.Equal(t, metadata, got)
	assert.Contains(t, readFile(t, path), "\n---\n\n# x\n\n| table |\n\n## Triage\n\nThe cause.\n", "the body survives")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	return string(data)
}
