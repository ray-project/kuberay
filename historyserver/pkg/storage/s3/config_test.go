package s3

import (
	"testing"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

func TestConfigCompleteFallsBackToLegacyAWSEnvVars(t *testing.T) {
	t.Setenv("AWS_S3ID", "")
	t.Setenv("AWS_S3SECRET", "")
	t.Setenv("AWS_S3TOKEN", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "legacy-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "legacy-secret")
	t.Setenv("AWS_SESSION_TOKEN", "legacy-token")

	cfg := &config{}
	cfg.complete(&types.RayCollectorConfig{}, nil)

	if cfg.S3ID != "legacy-id" {
		t.Fatalf("expected legacy access key fallback, got %q", cfg.S3ID)
	}
	if cfg.S3Secret != "legacy-secret" {
		t.Fatalf("expected legacy secret fallback, got %q", cfg.S3Secret)
	}
	if cfg.S3Token != "legacy-token" {
		t.Fatalf("expected legacy token fallback, got %q", cfg.S3Token)
	}
}

func TestConfigCompletePrefersNewS3EnvVars(t *testing.T) {
	t.Setenv("AWS_S3ID", "new-id")
	t.Setenv("AWS_S3SECRET", "new-secret")
	t.Setenv("AWS_S3TOKEN", "new-token")
	t.Setenv("AWS_ACCESS_KEY_ID", "legacy-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "legacy-secret")
	t.Setenv("AWS_SESSION_TOKEN", "legacy-token")

	cfg := &config{}
	cfg.complete(&types.RayCollectorConfig{}, nil)

	if cfg.S3ID != "new-id" {
		t.Fatalf("expected new access key to win, got %q", cfg.S3ID)
	}
	if cfg.S3Secret != "new-secret" {
		t.Fatalf("expected new secret to win, got %q", cfg.S3Secret)
	}
	if cfg.S3Token != "new-token" {
		t.Fatalf("expected new token to win, got %q", cfg.S3Token)
	}
}
