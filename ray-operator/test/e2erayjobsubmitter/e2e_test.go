package e2erayjobsubmitter

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/rayjobsubmitter"
)

var script = `import ray
import os

ray.init()

@ray.remote
class Counter:
    def __init__(self):
        # Used to verify runtimeEnv
        self.name = os.getenv("counter_name")
        assert self.name == "test_counter"
        self.counter = 0

    def inc(self):
        self.counter += 1

    def get_counter(self):
        return "{} got {}".format(self.name, self.counter)

counter = Counter.remote()

for _ in range(5):
    ray.get(counter.inc.remote())
    print(ray.get(counter.get_counter.remote()))
`

func TestRayJobSubmitter(t *testing.T) {
	// Create a temp job script
	scriptpy, err := os.CreateTemp("", "counter.py")
	if err != nil {
		t.Fatalf("Failed to create job script: %v", err)
	}
	defer func() { _ = os.Remove(scriptpy.Name()) }()
	if _, err = scriptpy.WriteString(script); err != nil {
		t.Fatalf("Failed to write to job script: %v", err)
	}
	if err = scriptpy.Close(); err != nil {
		t.Fatalf("Failed to close job script: %v", err)
	}

	// start ray
	cmd := exec.Command("ray", "start", "--head", "--disable-usage-stats", "--include-dashboard=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to start ray head: %v", err)
	}
	t.Log(string(out))
	if cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("Failed to start ray head with exit code: %v", cmd.ProcessState.ExitCode())
	}
	defer func() {
		cmd := exec.Command("ray", "stop")
		if _, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Failed to stop ray head: %v", err)
		}
	}()

	var address string
	re := regexp.MustCompile(`RAY_ADDRESS='([^']+)'`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) > 1 {
		address = matches[1]
	} else {
		t.Fatalf("Failed to find RAY_ADDRESS from the ray start output")
	}

	testcases := []struct {
		name string
		out  string
		req  utils.RayJobRequest
	}{
		{
			name: "my-job-1",
			req: utils.RayJobRequest{
				Entrypoint:   "python " + scriptpy.Name(),
				RuntimeEnv:   map[string]interface{}{"env_vars": map[string]string{"counter_name": "test_counter"}},
				SubmissionId: "my-job-1",
			},
			out: "test_counter got 5",
		},
		{
			name: "my-job-1-duplicated",
			req: utils.RayJobRequest{
				Entrypoint:   "python " + scriptpy.Name(),
				RuntimeEnv:   map[string]interface{}{"env_vars": map[string]string{"counter_name": "test_counter"}},
				SubmissionId: "my-job-1",
			},
			out: "test_counter got 5",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			out := bytes.NewBuffer(nil)

			rayjobsubmitter.Submit(address, tc.req, out)
			for _, expected := range []string{
				tc.out,
				fmt.Sprintf("Job '%s' succeeded", tc.req.SubmissionId),
			} {
				if !strings.Contains(out.String(), tc.out) {
					t.Errorf("Output did not contain expected string. output=%s\nexpected=%s\n", out.String(), expected)
				}
			}
		})
	}
}
