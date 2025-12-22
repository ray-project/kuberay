package s3

import (
	"os"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

type config struct {
	S3ForcePathStyle *bool
	DisableSSL       *bool
	S3Endpoint       string
	S3Bucket         string
	S3Region         string
	S3ID             string
	S3Secret         string
	S3Token          string
	types.RayCollectorConfig
}

func (c *config) complete(rcc *types.RayCollectorConfig, jd map[string]interface{}) {
	c.RayCollectorConfig = *rcc
	c.S3ID = os.Getenv("AWS_S3ID")
	c.S3Secret = os.Getenv("AWS_S3SECRET")
	c.S3Token = os.Getenv("AWS_S3TOKEN")
	if len(jd) == 0 {
		c.S3Bucket = os.Getenv("S3_BUCKET")
		c.S3Endpoint = os.Getenv("S3_ENDPOINT")
		c.S3Region = os.Getenv("S3_REGION")
		if os.Getenv("S3FORCE_PATH_STYLE") != "" {
			c.S3ForcePathStyle = aws.Bool(os.Getenv("S3FORCE_PATH_STYLE") == "true")
		}
		if os.Getenv("s3DisableSSL") != "" {
			c.DisableSSL = aws.Bool(os.Getenv("s3DisableSSL") == "true")
		}
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
			c.S3ForcePathStyle = aws.Bool(forcePathStyle.(string) == "true")
		}
		if s3disableSSL, ok := jd["s3DisableSSLs3DisableSSL"]; ok {
			c.DisableSSL = aws.Bool(s3disableSSL.(string) == "true")
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
	if len(jd) == 0 {
		c.S3Bucket = os.Getenv("S3_BUCKET")
		c.S3Endpoint = os.Getenv("S3_ENDPOINT")
		c.S3Region = os.Getenv("S3_REGION")
		if os.Getenv("S3FORCE_PATH_STYLE") != "" {
			c.S3ForcePathStyle = aws.Bool(os.Getenv("S3FORCE_PATH_STYLE") == "true")
		}
		if os.Getenv("s3DisableSSL") != "" {
			c.DisableSSL = aws.Bool(os.Getenv("s3DisableSSL") == "true")
		}
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
			c.S3ForcePathStyle = aws.Bool(forcePathStyle.(string) == "true")
		}
		if s3disableSSL, ok := jd["s3DisableSSL"]; ok {
			c.DisableSSL = aws.Bool(s3disableSSL.(string) == "true")
		}
	}
}
