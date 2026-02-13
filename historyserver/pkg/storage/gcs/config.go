package gcs

import (
	"os"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

type config struct {
	Bucket string
	types.RayCollectorConfig
}

func (c *config) completeCollectorConfig(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	if len(jd) != 0 {
		if bucket, ok := jd["gcsBucket"]; ok {
			c.Bucket = bucket.(string)
		}
	}
	if c.Bucket == "" {
		c.Bucket = os.Getenv("GCS_BUCKET")
	}
}

func (c *config) completeHistoryServerConfig(rcc *types.RayHistoryServerConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = types.RayCollectorConfig{
		RootDir: rcc.RootDir,
	}
	if len(jd) != 0 {
		if bucket, ok := jd["gcsBucket"]; ok {
			c.Bucket = bucket.(string)
		}
	}
	if c.Bucket == "" {
		c.Bucket = os.Getenv("GCS_BUCKET")
	}
}
