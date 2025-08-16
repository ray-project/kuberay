package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	flag "github.com/spf13/pflag"

	"github.com/ray-project/kuberay/ray-operator/rayjobsubmitter"
)

func main() {
	var (
		runtimeEnvJson      string
		metadataJson        string
		entrypointResources string
		entrypointNumCpus   float32
		entrypointNumGpus   float32
	)

	flag.StringVar(&runtimeEnvJson, "runtime-env-json", "", "")
	flag.StringVar(&metadataJson, "metadata-json", "", "")
	flag.StringVar(&entrypointResources, "entrypoint-resources", "", "")
	flag.Float32Var(&entrypointNumCpus, "entrypoint-num-cpus", 0.0, "")
	flag.Float32Var(&entrypointNumGpus, "entrypoint-num-gpus", 0.0, "")
	flag.Parse()

	address := os.Getenv("RAY_DASHBOARD_ADDRESS")
	if address == "" {
		exitOnError(fmt.Errorf("missing RAY_DASHBOARD_ADDRESS"))
	}
	submissionId := os.Getenv("RAY_JOB_SUBMISSION_ID")
	if submissionId == "" {
		exitOnError(fmt.Errorf("missing RAY_JOB_SUBMISSION_ID"))
	}

	req := rayjobsubmitter.RayJobRequest{
		Entrypoint:   strings.Join(flag.Args(), " "),
		SubmissionId: submissionId,
		NumCpus:      entrypointNumCpus,
		NumGpus:      entrypointNumGpus,
	}
	if len(runtimeEnvJson) > 0 {
		if err := json.Unmarshal([]byte(runtimeEnvJson), &req.RuntimeEnv); err != nil {
			exitOnError(err)
		}
	}
	if len(metadataJson) > 0 {
		if err := json.Unmarshal([]byte(metadataJson), &req.Metadata); err != nil {
			exitOnError(err)
		}
	}
	if len(entrypointResources) > 0 {
		if err := json.Unmarshal([]byte(entrypointResources), &req.Resources); err != nil {
			exitOnError(err)
		}
	}
	err := rayjobsubmitter.Submit(address, req, os.Stdout)
	exitOnError(err)
}

func exitOnError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR -- ", err)
		os.Exit(1)
	}
}
