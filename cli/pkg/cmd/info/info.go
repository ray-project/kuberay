package info

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/ray-project/kuberay/cli/pkg/cmd/version"
	"github.com/spf13/cobra"
)

func NewCmdInfo() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Output the version of kuberay, and OS info",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			info := GetInfo()
			fmt.Printf("KubeRay version: %s\n", info.KubeRayVersion)
			fmt.Printf("OS: %s\n", info.OS)
		},
	}

	return cmd
}

// Info holds versions info
type Info struct {
	KubeRayVersion string
	OS             string
}

// GetInfo returns versions info
func GetInfo() Info {
	return Info{
		KubeRayVersion: getKubeRayVersion(),
		OS:             runtime.GOOS,
	}
}

// getKubeRayVersion returns the kuberay version
func getKubeRayVersion() string {
	return version.GetVersion()
}

// String return info as JSON
func String() string {
	data, err := json.Marshal(GetInfo())
	if err != nil {
		return fmt.Sprintf("failed to marshal info into json: %q", err)
	}

	return string(data)
}
