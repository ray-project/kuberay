package support

import (
	"context"
	"os"
	"path"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

type Test interface {
	T() *testing.T
	Ctx() context.Context
	Client() Client
	OutputDir() string

	NewTestNamespace(...Option[*corev1.Namespace]) *corev1.Namespace
}

type Option[T any] interface {
	applyTo(to T) error
}

type errorOption[T any] func(to T) error

func (o errorOption[T]) applyTo(to T) error {
	return o(to)
}

var _ Option[any] = errorOption[any](nil)

func With(t *testing.T) Test {
	t.Helper()
	ctx := context.Background()
	if deadline, ok := t.Deadline(); ok {
		withDeadline, cancel := context.WithDeadline(ctx, deadline)
		t.Cleanup(cancel)
		ctx = withDeadline
	}

	return &T{
		t:   t,
		ctx: ctx,
	}
}

type T struct {
	t *testing.T
	//nolint:containedctx //nolint:nolintlint // TODO: The reason for this lint is unknown
	ctx       context.Context
	client    Client
	outputDir string
	once      struct {
		client    sync.Once
		outputDir sync.Once
	}
}

func (t *T) T() *testing.T {
	return t.t
}

func (t *T) Ctx() context.Context {
	return t.ctx
}

func (t *T) Client() Client {
	t.T().Helper()
	t.once.client.Do(func() {
		c, err := newTestClient()
		if err != nil {
			t.T().Fatalf("Error creating client: %v", err)
		}
		t.client = c
	})
	return t.client
}

func (t *T) OutputDir() string {
	t.T().Helper()
	t.once.outputDir.Do(func() {
		if parent, ok := os.LookupEnv(KuberayTestOutputDir); ok {
			if !path.IsAbs(parent) {
				if cwd, err := os.Getwd(); err == nil {
					// best effort to output the parent absolute path
					parent = path.Join(cwd, parent)
				}
			}
			t.T().Logf("Creating output directory in parent directory: %s", parent)
			dir, err := os.MkdirTemp(parent, t.T().Name())
			if err != nil {
				t.T().Fatalf("Error creating output directory: %v", err)
			}
			t.outputDir = dir
		} else {
			t.T().Logf("Creating ephemeral output directory as %s env variable is unset", KuberayTestOutputDir)
			t.outputDir = t.T().TempDir()
		}
		t.T().Logf("Output directory has been created at: %s", t.outputDir)
	})
	return t.outputDir
}

func (t *T) NewTestNamespace(options ...Option[*corev1.Namespace]) *corev1.Namespace {
	t.T().Helper()
	namespace := createTestNamespace(t, options...)
	t.T().Cleanup(func() {
		storeAllPodLogs(t, namespace)
		storeEvents(t, namespace)
		deleteTestNamespace(t, namespace)
	})
	return namespace
}
