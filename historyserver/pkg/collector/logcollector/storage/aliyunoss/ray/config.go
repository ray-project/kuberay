package ray

import (
	"os"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

type config struct {
	types.RayCollectorConfig

	OSSEndpoint string
	OSSBucket   string
}

func (c *config) complete(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	if len(jd) == 0 {
		c.OSSBucket = os.Getenv("OSS_BUCKET")
		c.OSSEndpoint = os.Getenv("OSS_ENDPOINT")
	} else {
		c.OSSBucket = jd["ossBucket"].(string)
		c.OSSEndpoint = jd["ossEndpoint"].(string)
	}
}

func (c *config) completeHSConfig(rcc *types.RayHistoryServerConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = types.RayCollectorConfig{
		RootDir: rcc.RootDir,
	}
	if len(jd) == 0 {
		c.OSSBucket = os.Getenv("OSS_BUCKET")
		c.OSSEndpoint = os.Getenv("OSS_ENDPOINT")
	} else {
		c.OSSBucket = jd["ossBucket"].(string)
		c.OSSEndpoint = jd["ossEndpoint"].(string)
	}
}
