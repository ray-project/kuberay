package e2e

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:embed resources/*.py
var files embed.FS

// CreateHttpRequest instantiates a http request for the  specified endpoint and host
func CreateHttpRequest(method string, host string, endPoint string, body io.Reader) (*http.Request, error) {
	url := host + endPoint
	req, err := http.NewRequestWithContext(context.TODO(), method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	return req, nil
}

// MakeBodyReader creates a io.Reader from the supplied string if is not empty after
// trimming the spaces
func MakeBodyReader(s string) io.Reader {
	if strings.TrimSpace(s) != "" {
		return strings.NewReader(s)
	}
	return nil
}

// PrettyPrintResponseBody generates a "pretty" formatted JSON string from the body
func PrettyPrintResponseBody(body io.ReadCloser) (string, error) {
	inputBytez, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, inputBytez, "", "\t"); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

func ReadFileAsString(t *testing.T, fileName string) string {
	file, err := files.ReadFile(fileName)
	require.NoErrorf(t, err, "No error expected when reading embedded file: '%s'", fileName)
	return string(file)
}
