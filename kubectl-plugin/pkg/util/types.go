package util

import (
	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type ResourceType string

const (
	RayCluster ResourceType = "raycluster"
	RayJob     ResourceType = "rayjob"
	RayService ResourceType = "rayservice"
)

type KubectlPluginCommonOptions struct {
	CmdFactory          cmdutil.Factory
	IoStreams           *genericiooptions.IOStreams
	Namespace           string
	WorkerNodeSelectors map[string]string
	RayVersion          string
	WorkerCPU           string
	WorkerGPU           string
	WorkerMemory        string
	WorkerReplicas      int32
}
