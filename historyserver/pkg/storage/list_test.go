package storage

import (
	"io"
	"slices"
	"testing"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

type mockStorageReader struct {
	files map[string][]string
}

func (m *mockStorageReader) List() []utils.ClusterInfo {
	return nil
}

func (m *mockStorageReader) GetContent(clusterId string, fileName string) io.Reader {
	return nil
}

func (m *mockStorageReader) ListFiles(clusterId string, dir string) []string {
	if entries, ok := m.files[dir]; ok {
		return entries
	}
	return nil
}

func TestListSessionNodeDirs(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		dirEntries  []string
		expected    []string
	}{
		{
			name:        "returns node directories",
			sessionName: "session-1",
			dirEntries:  []string{"node-a/", "node-b/"},
			expected:    []string{"node-a", "node-b"},
		},
		{
			name:        "skips files and fetched_endpoints",
			sessionName: "session-1",
			dirEntries: []string{
				"node-a/",
				"file.txt",
				utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME + "/",
				"/",
			},
			expected: []string{"node-a"},
		},
		{
			name:        "empty listing",
			sessionName: "session-1",
			dirEntries:  nil,
			expected:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader := &mockStorageReader{
				files: map[string][]string{
					tc.sessionName: tc.dirEntries,
				},
			}
			got := ListSessionNodeDirs(reader, "prefix", tc.sessionName)
			if !slices.Equal(got, tc.expected) {
				t.Errorf("ListSessionNodeDirs() = %v, want %v", got, tc.expected)
			}
		})
	}
}
