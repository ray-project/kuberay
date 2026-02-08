package historyserver

import (
	"strings"
	"testing"
	"time"
)

func TestFormatStatus(t *testing.T) {
	builder := NewClusterStatusBuilder()
	builder.Timestamp = time.Date(2026, 1, 20, 22, 55, 56, 762825000, time.UTC)

	debugState := &NodeDebugState{
		NodeID:    "abc123",
		NodeGroup: "headgroup",
		IsIdle:    true,
		Total: map[string]float64{
			"memory":              10000000000, // 10GB
			"object_store_memory": 1450000000,  // ~1.35GB
		},
		Available: map[string]float64{
			"memory":              10000000000, // All available (0 used)
			"object_store_memory": 1450000000,
		},
	}
	builder.AddNodeFromDebugState(debugState)

	status := builder.FormatStatus()

	if !strings.Contains(status, "======== Autoscaler status: 2026-01-20 22:55:56") {
		t.Errorf("Expected status to contain timestamp header")
	}

	if !strings.Contains(status, "Idle:\n 1 headgroup") {
		t.Errorf("Expected status to show 1 idle headgroup node, got:\n%s", status)
	}

	if !strings.Contains(status, "(no active nodes)") {
		t.Errorf("Expected status to show no active nodes")
	}

	if !strings.Contains(status, "memory") {
		t.Errorf("Expected status to contain memory resources")
	}

	t.Logf("Generated status:\n%s", status)
}

func TestFormatBytes(t *testing.T) {
	testcases := []struct {
		name     string
		bytes    float64
		expected string
	}{
		{"bytes", 512, "512B"},
		{"KiB", 1024, "1.00KiB"},
		{"MiB", 1024 * 1024, "1.00MiB"},
		{"GiB", 1.5 * 1024 * 1024 * 1024, "1.50GiB"},
		{"Zero", 0, "0B"},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatBytes(tc.bytes)
			if got != tc.expected {
				t.Fatalf("formatBytes(%f) = %q, want %q", tc.bytes, got, tc.expected)
			}
		})
	}
}
