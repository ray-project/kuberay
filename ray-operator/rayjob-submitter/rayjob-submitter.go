package rayjobsubmitter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	logTailingEndpoint    = "/logs/tail"
	jobSubmissionEndpoint = "/api/jobs/" // the tailing "/" is required.
	jobSubmissionTimeout  = 10 * time.Second
)

// RayJobRequest is the request body for submitting a Ray job.
// It is used to submit a Ray job to the Ray job submission server.
// To reduce the size of the binary, we don't include the utils package in the rayjobsubmitter package.
// The RayJobRequest struct should be the same as the RayJobRequest struct in the ray-operator/controllers/ray/utils/dashboard_httpclient.go file.
type RayJobRequest struct {
	RuntimeEnv   map[string]interface{} `json:"runtime_env,omitempty"`
	Metadata     map[string]string      `json:"metadata,omitempty"`
	Resources    map[string]float32     `json:"entrypoint_resources,omitempty"`
	Entrypoint   string                 `json:"entrypoint"`
	SubmissionId string                 `json:"submission_id,omitempty"`
	NumCpus      float32                `json:"entrypoint_num_cpus,omitempty"`
	NumGpus      float32                `json:"entrypoint_num_gpus,omitempty"`
}

func submitJobReq(address string, request RayJobRequest) (jobId string, err error) {
	rayJobJson, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), jobSubmissionTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewBuffer(rayJobJson))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// If the submission_id is already used,dashboard will return status code 400, we return the submission_id directly.
		if strings.Contains(string(body), "Please use a different submission_id") {
			return request.SubmissionId, nil
		}
		return "", fmt.Errorf("SubmitJob fail: %s %s", resp.Status, string(body))
	}

	return request.SubmissionId, nil
}

func jobSubmissionURL(address string) (string, error) {
	if !strings.HasPrefix(address, "http://") {
		address = "http://" + address
	}
	address, err := url.JoinPath(address, jobSubmissionEndpoint)
	if err != nil {
		return "", err
	}
	return address, nil
}

func logTailingURL(address, submissionId string) (string, error) {
	address = strings.Replace(address, "http", "ws", 1)
	address, err := url.JoinPath(address, submissionId, logTailingEndpoint)
	if err != nil {
		return "", err
	}
	return address, nil
}

func logJob(address, submissionId string, out io.Writer) error {
	wsAddr, err := logTailingURL(address, submissionId)
	if err != nil {
		return err
	}
	c, _, err := websocket.Dial(context.Background(), wsAddr, nil)
	if err != nil {
		return err
	}
	defer func() { _ = c.CloseNow() }()
	for {
		_, msg, err := c.Read(context.Background())
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				fmt.Fprintf(out, "SUCC -- Job '%s' succeeded\n", submissionId)
				return nil
			}
			return err
		}
		_, err = out.Write(msg)
		if err != nil {
			return err
		}
	}
}

func Submit(address string, req RayJobRequest, out io.Writer) error {
	fmt.Fprintf(out, "INFO -- Job submission server address: %s\n", address)

	address, err := jobSubmissionURL(address)
	if err != nil {
		return err
	}
	submissionId, err := submitJobReq(address, req)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "SUCC -- Job '%s' submitted successfully\n", submissionId)
	fmt.Fprintf(out, "INFO -- Tailing logs until the job exits (disable with --no-wait):\n")

	return logJob(address, submissionId, out)
}
