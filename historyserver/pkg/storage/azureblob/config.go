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

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

const DefaultContainerName = "ray-historyserver"

type config struct {
	ConnectionString string
	ContainerName    string
	Endpoint         string
	types.RayCollectorConfig
}

func getContainerNameWithDefault() string {
	container := os.Getenv("AZURE_STORAGE_CONTAINER")
	if container == "" {
		return DefaultContainerName
	}
	return container
}

func (c *config) complete(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	c.ConnectionString = os.Getenv("AZURE_STORAGE_CONNECTION_STRING")
	c.ContainerName = getContainerNameWithDefault()

	if len(jd) == 0 {
		c.Endpoint = os.Getenv("AZURE_STORAGE_ENDPOINT")
	} else {
		if container, ok := jd["azureContainer"]; ok {
			c.ContainerName = container.(string)
		}
		if endpoint, ok := jd["azureEndpoint"]; ok {
			c.Endpoint = endpoint.(string)
		}
		if connStr, ok := jd["azureConnectionString"]; ok {
			c.ConnectionString = connStr.(string)
		}
	}
}

func (c *config) completeHSConfig(rcc *types.RayHistoryServerConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = types.RayCollectorConfig{
		RootDir: rcc.RootDir,
	}
	c.ConnectionString = os.Getenv("AZURE_STORAGE_CONNECTION_STRING")
	c.ContainerName = getContainerNameWithDefault()

	if len(jd) == 0 {
		c.Endpoint = os.Getenv("AZURE_STORAGE_ENDPOINT")
	} else {
		if container, ok := jd["azureContainer"]; ok {
			c.ContainerName = container.(string)
		}
		if endpoint, ok := jd["azureEndpoint"]; ok {
			c.Endpoint = endpoint.(string)
		}
		if connStr, ok := jd["azureConnectionString"]; ok {
			c.ConnectionString = connStr.(string)
		}
	}
}
