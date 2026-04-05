// Package s3 is
/*
Copyright 2024 by the kuberay authors.

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
package s3

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/sirupsen/logrus"
)

func TestTrim(t *testing.T) {
	absoluteLogPathName := " /tmp/ray/test/LLogs/events///aa/a.txt  "
	logdir := "/tmp/ray/test/lLogs/"

	absoluteLogPathName = strings.TrimSpace(absoluteLogPathName)
	absoluteLogPathName = filepath.Clean(absoluteLogPathName)

	logdir = strings.TrimSpace(logdir)
	logdir = filepath.Clean(logdir)

	relativePath := strings.TrimPrefix(absoluteLogPathName, logdir+"/")
	// relativePath := strings.TrimPrefix(absoluteLogPathName, logdir)
	// Split relative path into subdir and filename
	subdir, filename := filepath.Split(relativePath)
	test_path_join := path.Join("aa./b/c/d", "e")
	t.Logf("file [%s] logdir [%s] subdir %s filename %s", absoluteLogPathName,
		logdir, subdir, filename)
	t.Logf("test_path_join [%s]", test_path_join)
}

func TestWalk(t *testing.T) {
	watchPath := "/tmp/ray/test/LLogs/"
	filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logrus.Errorf("Walk path error %v", err)
			return err // Return error
		}
		// Check if it's a file
		if !info.IsDir() {
			logrus.Infof("Find new file %s", path) // Log file path
		}
		return nil
	})
}

func TestReadFileFromFullPath(t *testing.T) {
	expectedContent := "RayJob_my-job"
	objectKey := "path/to/my/object"
	bucketName := "test-bucket"

	// Start a local HTTP server to mock S3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request is correct
		if r.URL.Path != "/"+bucketName+"/"+objectKey {
			t.Errorf("expected path /%s/%s, got %s", bucketName, objectKey, r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expectedContent))
	}))
	defer server.Close()

	// Configure a real S3 client to use the mock server as its endpoint
	sess, _ := session.NewSession(&aws.Config{
		Endpoint:         aws.String(server.URL),
		Region:           aws.String("us-east-1"),
		S3ForcePathStyle: aws.Bool(true), // Need is needed for local testing
		Credentials:      credentials.NewStaticCredentials("mock_id", "mock_secret", ""),
	})

	handler := &RayLogsHandler{
		S3Client: s3.New(sess),
		S3Bucket: bucketName,
	}

	content, err := handler.ReadFileFromFullPath(objectKey)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, content)
	}
}
