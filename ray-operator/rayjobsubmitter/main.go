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

	"github.com/coder/websocket"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func submitJobReq(address string, request utils.RayJobRequest) (jobId string, err error) {
	rayJobJson, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, address, bytes.NewBuffer(rayJobJson))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if strings.Contains(string(body), "Please use a different submission_id") {
		return request.SubmissionId, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("SubmitJob fail: %s %s", resp.Status, string(body))
	}

	return request.SubmissionId, nil
}

func jobSubmissionURL(address string) string {
	if !strings.HasPrefix(address, "http://") {
		address = "http://" + address
	}
	address, err := url.JoinPath(address, "/api/jobs/") // the tailing "/" is required.
	if err != nil {
		panic(err)
	}
	return address
}

func logTailingURL(address, submissionId string) string {
	address = strings.Replace(address, "http", "ws", 1)
	address, err := url.JoinPath(address, submissionId, "/logs/tail")
	if err != nil {
		panic(err)
	}
	return address
}

func Submit(address string, req utils.RayJobRequest, out io.Writer) {
	_, _ = fmt.Fprintf(out, "INFO -- Job submission server address: %s\n", address)

	address = jobSubmissionURL(address)
	submissionId, err := submitJobReq(address, req)
	if err != nil {
		panic(err)
	}

	_, _ = fmt.Fprintf(out, "SUCC -- Job '%s' submitted successfully\n", submissionId)
	_, _ = fmt.Fprintf(out, "INFO -- Tailing logs until the job exits (disable with --no-wait):\n")

	wsAddr := logTailingURL(address, submissionId)
	c, _, err := websocket.Dial(context.Background(), wsAddr, nil)
	if err != nil {
		panic(err)
	}
	defer func() { _ = c.CloseNow() }()
	for {
		_, msg, err := c.Read(context.Background())
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				_, _ = fmt.Fprintf(out, "SUCC -- Job '%s' succeeded\n", submissionId)
				return
			}
			panic(err)
		}
		_, _ = out.Write(msg)
	}
}
