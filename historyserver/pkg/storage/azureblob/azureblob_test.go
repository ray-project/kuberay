/*
Copyright 2024 The KubeRay Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package azureblob

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

// Azurite well-known connection string for local testing
const azuriteConnectionString = "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;"

func skipIfNoAzurite(t *testing.T) {
	t.Helper()

	// Try to connect to Azurite
	client, err := azblob.NewClientFromConnectionString(azuriteConnectionString, nil)
	if err != nil {
		t.Skipf("Skipping test: failed to create Azure client: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try to list containers to verify connection
	pager := client.NewListContainersPager(nil)
	_, err = pager.NextPage(ctx)
	if err != nil {
		t.Skipf("Skipping test: Azurite not available at localhost:10000: %v", err)
	}
}

func setupTestHandler(t *testing.T, containerName string) *RayLogsHandler {
	t.Helper()

	os.Setenv("AZURE_STORAGE_CONNECTION_STRING", azuriteConnectionString)
	os.Setenv("AZURE_STORAGE_CONTAINER", containerName)
	defer func() {
		os.Unsetenv("AZURE_STORAGE_CONNECTION_STRING")
		os.Unsetenv("AZURE_STORAGE_CONTAINER")
	}()

	cfg := &config{}
	cfg.complete(&types.RayCollectorConfig{
		RootDir:        "log",
		SessionDir:     "/tmp/ray/session_latest",
		RayClusterName: "test-cluster",
		RayClusterID:   "test-id",
		RayNodeName:    "test-node",
	}, nil)

	handler, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	return handler
}

func cleanupContainer(t *testing.T, containerName string) {
	t.Helper()

	client, err := azblob.NewClientFromConnectionString(azuriteConnectionString, nil)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	containerClient := client.ServiceClient().NewContainerClient(containerName)

	// List and delete all blobs
	pager := containerClient.NewListBlobsFlatPager(nil)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			break
		}
		for _, blob := range resp.Segment.BlobItems {
			blobClient := containerClient.NewBlobClient(*blob.Name)
			_, _ = blobClient.Delete(ctx, nil)
		}
	}

	// Delete container
	_, _ = containerClient.Delete(ctx, nil)
}

func TestCreateDirectory(t *testing.T) {
	skipIfNoAzurite(t)

	containerName := fmt.Sprintf("test-dir-%d", time.Now().UnixNano())
	defer cleanupContainer(t, containerName)

	handler := setupTestHandler(t, containerName)

	tests := []struct {
		name    string
		dirPath string
		wantErr bool
	}{
		{
			name:    "create simple directory",
			dirPath: "testdir",
			wantErr: false,
		},
		{
			name:    "create nested directory",
			dirPath: "parent/child/grandchild",
			wantErr: false,
		},
		{
			name:    "create directory with trailing slash",
			dirPath: "anotherdir/",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.CreateDirectory(tt.dirPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateDirectory() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWriteAndReadFile(t *testing.T) {
	skipIfNoAzurite(t)

	containerName := fmt.Sprintf("test-rw-%d", time.Now().UnixNano())
	defer cleanupContainer(t, containerName)

	handler := setupTestHandler(t, containerName)

	tests := []struct {
		name     string
		filePath string
		content  string
	}{
		{
			name:     "simple file",
			filePath: "log/test-cluster_test-id/session_1/logs/test.log",
			content:  "Hello, Azure Blob Storage!",
		},
		{
			name:     "file with special characters in content",
			filePath: "log/test-cluster_test-id/session_1/logs/special.log",
			content:  "Line 1\nLine 2\nLine 3\n日本語テスト",
		},
		{
			name:     "empty file",
			filePath: "log/test-cluster_test-id/session_1/logs/empty.log",
			content:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write file
			reader := bytes.NewReader([]byte(tt.content))
			err := handler.WriteFile(tt.filePath, reader)
			if err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			// Read file back
			// Need to extract clusterId and fileName from the path
			parts := strings.SplitN(tt.filePath, "/", 2)
			if len(parts) < 2 {
				t.Fatalf("Invalid file path format: %s", tt.filePath)
			}

			// For GetContent, we need to work with the path structure
			result := handler.GetContent(parts[1], "")
			if result == nil && tt.content != "" {
				// Try direct read from the handler's context
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				blobClient := handler.ContainerClient.NewBlobClient(tt.filePath)
				resp, err := blobClient.DownloadStream(ctx, nil)
				if err != nil {
					t.Fatalf("Failed to read back file: %v", err)
				}
				defer resp.Body.Close()

				data, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("Failed to read response body: %v", err)
				}

				if string(data) != tt.content {
					t.Errorf("Content mismatch: got %q, want %q", string(data), tt.content)
				}
			}
		})
	}
}

func TestListFiles(t *testing.T) {
	skipIfNoAzurite(t)

	containerName := fmt.Sprintf("test-list-%d", time.Now().UnixNano())
	defer cleanupContainer(t, containerName)

	handler := setupTestHandler(t, containerName)

	// Create some test files
	testFiles := []string{
		"log/cluster1_id1/session_1/logs/file1.log",
		"log/cluster1_id1/session_1/logs/file2.log",
		"log/cluster1_id1/session_1/events/event1.json",
	}

	for _, file := range testFiles {
		reader := bytes.NewReader([]byte("test content"))
		if err := handler.WriteFile(file, reader); err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	// List files in logs directory
	files := handler.ListFiles("cluster1_id1/session_1", "logs")
	t.Logf("Listed files: %v", files)

	if len(files) == 0 {
		t.Error("Expected to find files but got none")
	}
}

func TestNewReader(t *testing.T) {
	skipIfNoAzurite(t)

	os.Setenv("AZURE_STORAGE_CONNECTION_STRING", azuriteConnectionString)
	os.Setenv("AZURE_STORAGE_CONTAINER", fmt.Sprintf("test-reader-%d", time.Now().UnixNano()))
	defer func() {
		os.Unsetenv("AZURE_STORAGE_CONNECTION_STRING")
		os.Unsetenv("AZURE_STORAGE_CONTAINER")
	}()

	reader, err := NewReader(&types.RayHistoryServerConfig{
		RootDir: "log",
	}, nil)

	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}

	if reader == nil {
		t.Error("NewReader() returned nil")
	}
}

func TestNewWriter(t *testing.T) {
	skipIfNoAzurite(t)

	os.Setenv("AZURE_STORAGE_CONNECTION_STRING", azuriteConnectionString)
	os.Setenv("AZURE_STORAGE_CONTAINER", fmt.Sprintf("test-writer-%d", time.Now().UnixNano()))
	defer func() {
		os.Unsetenv("AZURE_STORAGE_CONNECTION_STRING")
		os.Unsetenv("AZURE_STORAGE_CONTAINER")
	}()

	writer, err := NewWriter(&types.RayCollectorConfig{
		RootDir:        "log",
		SessionDir:     "/tmp/ray/session_latest",
		RayClusterName: "test-cluster",
		RayClusterID:   "test-id",
		RayNodeName:    "test-node",
	}, nil)

	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}

	if writer == nil {
		t.Error("NewWriter() returned nil")
	}
}
