package s3

import (
	"fmt"
	"os"
	"strconv"

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

func (c *config) complete(rcc *types.RayCollectorConfig, jd map[string]interface{}) error {
	c.RayCollectorConfig = *rcc
	c.S3ID = os.Getenv("AWS_S3ID")
	c.S3Secret = os.Getenv("AWS_S3SECRET")
	c.S3Token = os.Getenv("AWS_S3TOKEN")
	c.S3Bucket = getS3BucketWithDefault()
	if len(jd) == 0 {
		c.S3Endpoint = os.Getenv("S3_ENDPOINT")
		c.S3Region = os.Getenv("S3_REGION")
		if err := setBoolFromEnv("S3FORCE_PATH_STYLE", &c.S3ForcePathStyle); err != nil {
			return err
		}
		if err := setBoolFromEnv("S3DISABLE_SSL", &c.DisableSSL); err != nil {
			return err
		}
	} else {
		if bucket, ok := jd["s3Bucket"]; ok {
			v, ok := bucket.(string)
			if !ok {
				return fmt.Errorf("invalid type %T for s3Bucket: expected string", bucket)
			}
			c.S3Bucket = v
		}
		if endpoint, ok := jd["s3Endpoint"]; ok {
			v, ok := endpoint.(string)
			if !ok {
				return fmt.Errorf("invalid type %T for s3Endpoint: expected string", endpoint)
			}
			c.S3Endpoint = v
		}
		if region, ok := jd["s3Region"]; ok {
			v, ok := region.(string)
			if !ok {
				return fmt.Errorf("invalid type %T for s3Region: expected string", region)
			}
			c.S3Region = v
		}
		if forcePathStyle, ok := jd["s3ForcePathStyle"]; ok {
			if err := setBoolFromValue("s3ForcePathStyle", forcePathStyle, &c.S3ForcePathStyle); err != nil {
				return err
			}
		}
		if s3disableSSL, ok := jd["s3DisableSSL"]; ok {
			if err := setBoolFromValue("s3DisableSSL", s3disableSSL, &c.DisableSSL); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *config) completeHSConfig(rcc *types.RayHistoryServerConfig, jd map[string]interface{}) error {
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
		if err := setBoolFromEnv("S3FORCE_PATH_STYLE", &c.S3ForcePathStyle); err != nil {
			return err
		}
		if err := setBoolFromEnv("S3DISABLE_SSL", &c.DisableSSL); err != nil {
			return err
		}
	} else {
		if bucket, ok := jd["s3Bucket"]; ok {
			v, ok := bucket.(string)
			if !ok {
				return fmt.Errorf("invalid type %T for s3Bucket: expected string", bucket)
			}
			c.S3Bucket = v
		}
		if endpoint, ok := jd["s3Endpoint"]; ok {
			v, ok := endpoint.(string)
			if !ok {
				return fmt.Errorf("invalid type %T for s3Endpoint: expected string", endpoint)
			}
			c.S3Endpoint = v
		}
		if region, ok := jd["s3Region"]; ok {
			v, ok := region.(string)
			if !ok {
				return fmt.Errorf("invalid type %T for s3Region: expected string", region)
			}
			c.S3Region = v
		}
		if forcePathStyle, ok := jd["s3ForcePathStyle"]; ok {
			if err := setBoolFromValue("s3ForcePathStyle", forcePathStyle, &c.S3ForcePathStyle); err != nil {
				return err
			}
		}
		if s3disableSSL, ok := jd["s3DisableSSL"]; ok {
			if err := setBoolFromValue("s3DisableSSL", s3disableSSL, &c.DisableSSL); err != nil {
				return err
			}
		}
	}
	return nil
}

func setBoolFromEnv(envKey string, target *bool) error {
	value, ok := os.LookupEnv(envKey)
	if !ok || value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("failed to parse boolean from environment variable %q: %w", envKey, err)
	}
	*target = parsed
	return nil
}

func setBoolFromValue(name string, value interface{}, target *bool) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case bool:
		*target = v
		return nil
	case string:
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("failed to parse boolean value for %q: %w", name, err)
		}
		*target = parsed
		return nil
	default:
		return fmt.Errorf("unsupported type %T for config field %q: expected bool or string", value, name)
	}
}
