package rayjobsubmitter

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

const (
	logTailingEndpoint    = "/logs/tail"
	jobSubmissionEndpoint = "/api/jobs/" // the tailing "/" is required.
)

func JobSubmissionURL(address string) string {
	if !strings.HasPrefix(address, "http://") {
		address = "http://" + address
	}
	return address
}

func logTailingURL(address, submissionId string) (string, error) {
	address = strings.Replace(address, "http", "ws", 1)
	address, err := url.JoinPath(address, jobSubmissionEndpoint, submissionId, logTailingEndpoint)
	if err != nil {
		return "", err
	}
	return address, nil
}

func TailJobLogs(address, submissionId string, out io.Writer) error {
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
