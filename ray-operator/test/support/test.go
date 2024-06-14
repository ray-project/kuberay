package support

import (
	"bufio"
	"context"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
)

type Test interface {
	T() *testing.T
	Ctx() context.Context
	Client() Client
	OutputDir() string

	gomega.Gomega

	NewTestNamespace(...Option[*corev1.Namespace]) *corev1.Namespace
	StreamKubeRayOperatorLogs()
}

type Option[T any] interface {
	applyTo(to T) error
}

type errorOption[T any] func(to T) error

//nolint:unused // To be removed when the false-positivity is fixed.
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
		WithT: gomega.NewWithT(t),
		t:     t,
		ctx:   ctx,
	}
}

type T struct {
	*gomega.WithT
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

func (t *T) StreamKubeRayOperatorLogs() {
	ctx, cancel := context.WithCancel(context.Background())
	t.T().Cleanup(cancel)
	// By using `.Pods("")`, we list kuberay-operators from all namespaces
	// because they may not always be installed in the "ray-system" namespace.
	pods, err := t.Client().Core().CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=kuberay-operator",
	})
	t.Expect(err).ShouldNot(gomega.HaveOccurred())
	now := metav1.NewTime(time.Now())
	for _, pod := range pods.Items {
		go func(pod corev1.Pod, ts *metav1.Time) {
			req := t.Client().Core().CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Follow:    true,
				SinceTime: ts,
			})
			stream, err := req.Stream(ctx)
			if err != nil {
				t.T().Logf("Fail to tail logs from the pod %s/%s", pod.Namespace, pod.Name)
				return
			}
			t.T().Logf("Start tailing logs from the pod %s/%s", pod.Namespace, pod.Name)
			defer stream.Close()
			scanner := bufio.NewScanner(stream)
			for scanner.Scan() {
				t.T().Log(scanner.Text())
			}
			t.T().Logf("Stop tailing logs from the pod %s/%s: %v", pod.Namespace, pod.Name, scanner.Err())
		}(pod, &now)
	}
}
