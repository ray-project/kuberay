package s3

import (
	"os"
	"strconv"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

const DefaultS3Bucket = "ray-historyserver"

type config struct {
	S3ForcePathStyle bool
	DisableSSL       bool
	S3Endpoint       string
	S3Bucket         string
	S3Region         string
	S3ID             string
	S3Secret         string
	S3Token          string
	types.RayCollectorConfig
}

func getS3BucketWithDefault() string {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		return DefaultS3Bucket
	}
	return bucket
}

func (c *config) complete(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	c.S3ID = os.Getenv("AWS_S3ID")
	c.S3Secret = os.Getenv("AWS_S3SECRET")
	c.S3Token = os.Getenv("AWS_S3TOKEN")
	c.S3Bucket = getS3BucketWithDefault()
	if len(jd) == 0 {
		c.S3Endpoint = os.Getenv("S3_ENDPOINT")
		c.S3Region = os.Getenv("S3_REGION")
		setBoolFromEnv("S3FORCE_PATH_STYLE", &c.S3ForcePathStyle)
		setBoolFromEnv("S3DISABLE_SSL", &c.DisableSSL)
	} else {
		if bucket, ok := jd["s3Bucket"]; ok {
			c.S3Bucket = bucket.(string)
		}
		if endpoint, ok := jd["s3Endpoint"]; ok {
			c.S3Endpoint = endpoint.(string)
		}
		if region, ok := jd["s3Region"]; ok {
			c.S3Region = region.(string)
		}
		if forcePathStyle, ok := jd["s3ForcePathStyle"]; ok {
			setBoolFromValue("s3ForcePathStyle", forcePathStyle, &c.S3ForcePathStyle)
		}
		if s3disableSSL, ok := jd["s3DisableSSL"]; ok {
			setBoolFromValue("s3DisableSSL", s3disableSSL, &c.DisableSSL)
		}
	}
}

func (c *config) completeHSConfig(rcc *types.RayHistoryServerConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = types.RayCollectorConfig{
		RootDir: rcc.RootDir,
	}
	c.S3ID = os.Getenv("AWS_S3ID")
	c.S3Secret = os.Getenv("AWS_S3SECRET")
	c.S3Token = os.Getenv("AWS_S3TOKEN")
	c.S3Bucket = getS3BucketWithDefault() // Use default if S3_BUCKET not set
	if len(jd) == 0 {
		c.S3Endpoint = os.Getenv("S3_ENDPOINT")
		c.S3Region = os.Getenv("S3_REGION")
		setBoolFromEnv("S3FORCE_PATH_STYLE", &c.S3ForcePathStyle)
		setBoolFromEnv("S3DISABLE_SSL", &c.DisableSSL)
	} else {
		if bucket, ok := jd["s3Bucket"]; ok {
			c.S3Bucket = bucket.(string)
		}
		if endpoint, ok := jd["s3Endpoint"]; ok {
			c.S3Endpoint = endpoint.(string)
		}
		if region, ok := jd["s3Region"]; ok {
			c.S3Region = region.(string)
		}
		if forcePathStyle, ok := jd["s3ForcePathStyle"]; ok {
			setBoolFromValue("s3ForcePathStyle", forcePathStyle, &c.S3ForcePathStyle)
		}
		if s3disableSSL, ok := jd["s3DisableSSL"]; ok {
			setBoolFromValue("s3DisableSSL", s3disableSSL, &c.DisableSSL)
		}
	}
}

func setBoolFromEnv(envKey string, target *bool) {
	value := os.Getenv(envKey)
	if value == "" {
		return
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		logrus.Warnf("Invalid boolean value %q for %s", value, envKey)
		return
	}
	*target = parsed
}

func setBoolFromValue(name string, value interface{}, target *bool) {
	if value == nil {
		return
	}
	switch v := value.(type) {
	case bool:
		*target = v
	case string:
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			logrus.Warnf("Invalid boolean value %q for %s", v, name)
			return
		}
		*target = parsed
	default:
		logrus.Warnf("Invalid boolean type %T for %s", value, name)
	}
}
