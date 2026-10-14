package util

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Paths are the files the commands read and write.
type Paths struct {
	Root     string // the repository
	Items    string
	Manifest string
	Cache    string // sources fetched by stale, read by the plan and triage skills
	Results  string // this version's results; set only by the commands that use them
}

// validTag: --kuberay-version names the results directory and is spliced into URLs.
var validTag = regexp.MustCompile(`^v` + ReleaseVersion + `$`)

func ValidateVersion(version string) error {
	if !validTag.MatchString(version) {
		return fmt.Errorf("--kuberay-version %q is not a release tag like v1.8.0-rc.0", version)
	}
	return nil
}

func FindPaths(ctx context.Context) (Paths, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return Paths{}, fmt.Errorf("run inside the kuberay repository: %w", err)
	}
	root := strings.TrimSpace(string(out))
	dir := filepath.Join(root, releaseTestDir)
	return Paths{
		Root:     root,
		Items:    filepath.Join(dir, "items.yaml"),
		Manifest: filepath.Join(dir, "manifest.yaml"),
		Cache:    filepath.Join(dir, ".cache"),
	}, nil
}

// ResultsDir is release-test/results/<version>: one directory per version, so a new candidate
// starts clean and the old one stays readable.
func (p Paths) ResultsDir(version string) string {
	return filepath.Join(p.Root, releaseTestDir, "results", version)
}

// Rel makes a path repository-relative, the way the skills refer to files.
func (p Paths) Rel(path string) string {
	if relative, err := filepath.Rel(p.Root, path); err == nil && filepath.IsLocal(relative) {
		return relative
	}
	return path
}
