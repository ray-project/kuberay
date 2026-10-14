package report

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

// FrontMatter heads results/<tag>/<id>/report.md; the runner writes it, triage fills in triage and brief.
type FrontMatter struct {
	Item       string `json:"item"`
	Status     string `json:"status"`
	Version    string `json:"version"`
	FailedStep *int   `json:"failed_step"`
	Triage     string `json:"triage"`
	Brief      string `json:"brief"`
}

func ReadFrontMatter(path string) (FrontMatter, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return FrontMatter{}, err
	}
	return parseFrontMatter(string(data))
}

func parseFrontMatter(report string) (FrontMatter, error) {
	var metadata FrontMatter
	rest, ok := strings.CutPrefix(report, "---\n")
	if !ok {
		return metadata, errors.New("no front matter")
	}
	head, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return metadata, errors.New("front matter is not closed")
	}
	err := yaml.Unmarshal([]byte(head), &metadata)
	return metadata, err
}

// front renders a report's front matter; triage fills in triage and brief.
func front(metadata FrontMatter) string {
	return Render("front.tmpl", metadata)
}

// WriteFrontMatter replaces the front matter of the report at path, keeping its body.
func WriteFrontMatter(path string, metadata FrontMatter) error {
	report, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	body := string(report)
	if rest, ok := strings.CutPrefix(body, "---\n"); ok {
		if _, after, ok := strings.Cut(rest, "\n---\n"); ok {
			body = after
		}
	}
	return os.WriteFile(path, []byte(front(metadata)+strings.TrimLeft(body, "\n")), 0o600) //nolint:gosec // the report this run wrote, under a validated id
}
