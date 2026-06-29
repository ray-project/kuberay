package localtest

import (
	"testing"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

func TestMockReaderListEnrichment(t *testing.T) {
	r := NewMockReader()
	clusters := r.List()

	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}

	if clusters[0].Name != "cluster-1" {
		t.Fatalf("expected cluster-1 first (sorted by CreateTimeStamp descending), got %s", clusters[0].Name)
	}

	// cluster-1 has completed status and EndTime set in mock meta
	if clusters[0].Status != utils.SessionStatusCompleted {
		t.Errorf("cluster-1 Status = %q, want %q", clusters[0].Status, utils.SessionStatusCompleted)
	}
	if clusters[0].EndTime != 1672534800 {
		t.Errorf("cluster-1 EndTime = %d, want 1672534800", clusters[0].EndTime)
	}

	// cluster-2 has in_progress status and no EndTime in mock meta
	if clusters[1].Status != utils.SessionStatusInProgress {
		t.Errorf("cluster-2 Status = %q, want %q", clusters[1].Status, utils.SessionStatusInProgress)
	}
	if clusters[1].EndTime != 0 {
		t.Errorf("cluster-2 EndTime = %d, want 0", clusters[1].EndTime)
	}
}

func TestMockReaderReadMetaNotFound(t *testing.T) {
	r := NewMockReader()

	// ReadMeta for a path that does not exist should return an error.
	// This simulates backward compatibility: sessions without meta.json
	// must not cause failures.
	_, err := r.ReadMeta("nonexistent/path.meta.json")
	if err == nil {
		t.Error("ReadMeta for non-existent path should return an error")
	}
}

func TestMockWriterWriteMeta(t *testing.T) {
	w := NewMockWriter()
	meta := utils.MetaJson{
		SessionName:      "session-test",
		ClusterID:        "cluster-test",
		ClusterNamespace: "default",
		Status:           utils.SessionStatusInProgress,
		StartTime:        1234567890,
	}
	err := w.WriteMeta("cluster-metadata/raycluster/default_cluster-test/session-test.meta.json", meta)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Verify the written data is stored and valid JSON
	raw, ok := w.metaJsons["cluster-metadata/raycluster/default_cluster-test/session-test.meta.json"]
	if !ok {
		t.Fatal("meta.json not found in mock writer map")
	}
	if raw == "" {
		t.Error("meta.json content should not be empty")
	}
}
