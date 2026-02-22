package s3

import (
	"testing"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

func TestSetBoolFromEnvReturnsErrorOnInvalidValue(t *testing.T) {
	t.Setenv("S3FORCE_PATH_STYLE", "not-a-bool")

	var target bool
	err := setBoolFromEnv("S3FORCE_PATH_STYLE", &target)
	if err == nil {
		t.Fatalf("expected error for invalid bool env value")
	}
}

func TestSetBoolFromValueReturnsErrorOnInvalidType(t *testing.T) {
	var target bool
	err := setBoolFromValue("s3DisableSSL", 123, &target)
	if err == nil {
		t.Fatalf("expected error for unsupported value type")
	}
}

func TestCompleteReturnsErrorOnInvalidEnvBool(t *testing.T) {
	t.Setenv("S3FORCE_PATH_STYLE", "not-a-bool")

	cfg := &config{}
	err := cfg.complete(&types.RayCollectorConfig{}, nil)
	if err == nil {
		t.Fatalf("expected error from complete on invalid env bool")
	}
}

func TestCompleteHSConfigReturnsErrorOnInvalidJSONBool(t *testing.T) {
	cfg := &config{}
	jd := map[string]interface{}{
		"s3DisableSSL": "not-a-bool",
	}

	err := cfg.completeHSConfig(&types.RayHistoryServerConfig{}, jd)
	if err == nil {
		t.Fatalf("expected error from completeHSConfig on invalid bool string")
	}
}
