package main

import (
	"encoding/json"
	"os"
	"strings"

	flag "github.com/spf13/pflag"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
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
		panic("Missing RAY_DASHBOARD_ADDRESS")
	}
	submissionId := os.Getenv("RAY_JOB_SUBMISSION_ID")
	if submissionId == "" {
		panic("Missing RAY_JOB_SUBMISSION_ID")
	}

	req := utils.RayJobRequest{
		Entrypoint:   strings.Join(flag.Args(), " "),
		SubmissionId: submissionId,
		NumCpus:      entrypointNumCpus,
		NumGpus:      entrypointNumGpus,
	}
	if len(runtimeEnvJson) > 0 {
		if err := json.Unmarshal([]byte(runtimeEnvJson), &req.RuntimeEnv); err != nil {
			panic(err)
		}
	}
	if len(metadataJson) > 0 {
		if err := json.Unmarshal([]byte(metadataJson), &req.Metadata); err != nil {
			panic(err)
		}
	}
	if len(entrypointResources) > 0 {
		if err := json.Unmarshal([]byte(entrypointResources), &req.Resources); err != nil {
			panic(err)
		}
	}
	rayjobsubmitter.Submit(address, req, os.Stdout)
}
