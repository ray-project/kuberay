package gcs

import (
	"os"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

const DefaultGCSBucket = "ray-historyserver"

type config struct {
	Bucket string
	types.RayCollectorConfig
}

func getGCSDefaultBucketName() string {
	bucket := os.Getenv("GCS_BUCKET")
	if bucket == "" {
		return DefaultGCSBucket
	}
	return bucket
}

func (c *config) completeCollectorConfig(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	c.Bucket = getGCSDefaultBucketName()
	if len(jd) != 0 {
		if bucket, ok := jd["gcsBucket"]; ok {
			c.Bucket = bucket.(string)
		}
	}
}

func (c *config) completeHistoryServerConfig(rcc *types.RayHistoryServerConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = types.RayCollectorConfig{
		RootDir: rcc.RootDir,
	}
	c.Bucket = getGCSDefaultBucketName()
	if len(jd) != 0 {
		if bucket, ok := jd["gcsBucket"]; ok {
			c.Bucket = bucket.(string)
		}
	}
}
