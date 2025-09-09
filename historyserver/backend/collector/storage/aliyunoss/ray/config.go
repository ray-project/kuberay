package ray

import "github.com/ray-project/kuberay/historyserver/backend/types"

type config struct {
	types.RayCollectorConfig

	OSSEndpoint string
	OSSBucket   string
}

func (c *config) complete(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	c.OSSBucket = jd["ossBucket"].(string)
	c.OSSEndpoint = jd["ossEndpoint"].(string)
}

func (c *config) completeHSConfig(rcc *types.RayHistoryServerConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = types.RayCollectorConfig{
		RootDir: rcc.RootDir,
	}
	c.OSSBucket = jd["ossBucket"].(string)
	c.OSSEndpoint = jd["ossEndpoint"].(string)
}
