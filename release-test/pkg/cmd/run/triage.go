package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/ray-project/kuberay/release-test/pkg/util"
	"github.com/ray-project/kuberay/release-test/pkg/util/report"
	"github.com/ray-project/kuberay/release-test/pkg/util/source"
)

// triageClasses are what the release-test-run skill can classify a failure as.
const triageClasses = `plan-bug|doc-bug|sample-bug|product-bug|env|flaky`

// triageUnit asks the release-test-run skill why the unit failed. The skill appends a Triage
// section to report.md and ends its reply with a verdict line, which this copies into metadata,
// the front matter the runner wrote: the skill never edits YAML. The page is prefetched into the
// cache.
func (r *runner) triageUnit(ctx context.Context, item util.Item, dir string, metadata report.FrontMatter) {
	_, _ = source.Fetch(ctx, r.path, item, r.version)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	dir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Println("  triage skipped:", err)
		return
	}
	reportPath := filepath.Join(dir, util.ReportFile)
	stdout, err1 := os.Create(filepath.Join(dir, util.TriageFile))
	stderr, err2 := os.Create(filepath.Join(dir, "triage.err"))
	if err := errors.Join(err1, err2); err != nil {
		fmt.Println("  triage skipped:", err)
		return
	}
	defer stdout.Close()
	defer stderr.Close()
	cmd := triageCommand(ctx, r.path.Root, r.model, dir, reportPath)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("  triage failed, see %s\n", stderr.Name())
		return
	}
	if info, err := stderr.Stat(); err == nil && info.Size() == 0 {
		_ = os.Remove(stderr.Name())
	}
	triage, brief, err := readVerdict(stdout.Name(), item.ID)
	if err != nil {
		fmt.Printf("  %v, see %s\n", err, stdout.Name())
		return
	}
	metadata.Triage, metadata.Brief = triage, brief
	if err := report.WriteFrontMatter(reportPath, metadata); err != nil {
		fmt.Println(" ", err)
		return
	}
	fmt.Printf("  %s: %s\n", triage, brief)
}

// triageCommand runs the release-test-run skill headless on a unit's results directory.
func triageCommand(ctx context.Context, root, model, dir, reportPath string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "claude", "-p", "/release-test-run "+dir, //nolint:gosec // fixed tool; dir is ours
		"--model", model, "--max-turns", "15", "--output-format", "json",
		"--permission-mode", "dontAsk",
		"--tools", "Read", "Grep", "Glob", "Edit",
		"--allowedTools", "Edit(//"+reportPath+")", // one slash would anchor at the settings source
		"--add-dir", dir,
		"--settings", `{"permissions":{"blockReadsOutsideWorkingDirectories":true}}`)
	cmd.Dir = root
	return cmd
}

// readVerdict finds the last `<id> <triage>: <brief>` line in the reply that the triage call's
// JSON output at path holds. A quoted brief is unquoted.
func readVerdict(path, id string) (triage, brief string, err error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", "", err
	}
	var output struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(data, &output); err != nil {
		return "", "", fmt.Errorf("triage output: %w", err)
	}
	line := regexp.MustCompile("(?m)^`?" + regexp.QuoteMeta(id) + " (" + triageClasses + "): (.+?)`?[ \t]*$")
	matches := line.FindAllStringSubmatch(output.Result, -1)
	if len(matches) == 0 {
		return "", "", errors.New("triage gave no verdict line")
	}
	last := matches[len(matches)-1]
	triage, brief = last[1], last[2]
	if unquoted, err := strconv.Unquote(brief); err == nil {
		brief = unquoted
	}
	return triage, brief, nil
}
