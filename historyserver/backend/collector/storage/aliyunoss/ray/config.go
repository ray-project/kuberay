package ray

import "github.com/ray-project/kuberay/historyserver/backend/types"

type config struct {
	types.RayCollectorConfig

	OSSEndpoint         string
	OSSBucket           string
	OSSHistoryServerDir string
}

func (c *config) complete(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	c.OSSBucket = jd["ossBucket"].(string)
	c.OSSBucket = jd["ossEndpoint"].(string)
	c.OSSHistoryServerDir = jd["ossHistoryServerDir"].(string)
}
