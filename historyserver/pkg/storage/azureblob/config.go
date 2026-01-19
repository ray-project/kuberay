/*
Copyright 2024 The KubeRay Authors.

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

package azureblob

import (
	"os"
	"strings"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

const DefaultContainerName = "ray-historyserver"

// AuthMode specifies the authentication method for Azure Blob Storage
type AuthMode string

const (
	// AuthModeConnectionString uses a connection string for authentication
	AuthModeConnectionString AuthMode = "connection_string"
	// AuthModeWorkloadIdentity uses Azure Workload Identity (for AKS)
	AuthModeWorkloadIdentity AuthMode = "workload_identity"
	// AuthModeDefault uses DefaultAzureCredential which tries multiple methods
	AuthModeDefault AuthMode = "default"
)

type config struct {
	AuthMode         AuthMode
	ConnectionString string
	AccountURL       string
	ContainerName    string
	types.RayCollectorConfig
}

func getContainerNameWithDefault() string {
	container := os.Getenv("AZURE_STORAGE_CONTAINER")
	if container == "" {
		return DefaultContainerName
	}
	return container
}

func getAuthMode() AuthMode {
	mode := strings.ToLower(os.Getenv("AZURE_STORAGE_AUTH_MODE"))
	switch mode {
	case "connection_string":
		return AuthModeConnectionString
	case "workload_identity":
		return AuthModeWorkloadIdentity
	case "default":
		return AuthModeDefault
	default:
		// Auto-detect based on available credentials
		return ""
	}
}

func (c *config) populateFromEnvAndJSON(jd map[string]interface{}) {
	c.ConnectionString = os.Getenv("AZURE_STORAGE_CONNECTION_STRING")
	c.AccountURL = os.Getenv("AZURE_STORAGE_ACCOUNT_URL")
	c.ContainerName = getContainerNameWithDefault()
	c.AuthMode = getAuthMode()

	if len(jd) > 0 {
		if v, ok := jd["azureContainer"]; ok {
			c.ContainerName = v.(string)
		}
		if v, ok := jd["azureConnectionString"]; ok {
			c.ConnectionString = v.(string)
		}
		if v, ok := jd["azureAccountURL"]; ok {
			c.AccountURL = v.(string)
		}
		if v, ok := jd["azureAuthMode"]; ok {
			c.AuthMode = AuthMode(v.(string))
		}
	}
}

func (c *config) complete(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	c.populateFromEnvAndJSON(jd)
}

func (c *config) completeHSConfig(rcc *types.RayHistoryServerConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = types.RayCollectorConfig{RootDir: rcc.RootDir}
	c.populateFromEnvAndJSON(jd)
}
