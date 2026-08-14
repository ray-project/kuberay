package historyserver

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	benchNamespace   = "default"
	benchClusterName = "bench-cluster"
)

// countingClient counts Get calls and injects a delay to model API server
// round-trip latency. Without a delay there is no in-flight window for
// concurrent callers to overlap in, so coalescing has nothing to coalesce.
type countingClient struct {
	client.Client
	gets  atomic.Int64
	delay time.Duration
}

func (c *countingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func newBenchClientManager(tb testing.TB, delay time.Duration) (*ClientManager, *countingClient) {
	tb.Helper()

	scheme := runtime.NewScheme()
	if err := rayv1.AddToScheme(scheme); err != nil {
		tb.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		tb.Fatal(err)
	}

	rc := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{Name: benchClusterName, Namespace: benchNamespace},
		Spec: rayv1.RayClusterSpec{
			AuthOptions: &rayv1.AuthOptions{Mode: rayv1.AuthModeToken},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: benchClusterName, Namespace: benchNamespace},
		Data:       map[string][]byte{AuthTokenSecretKey: []byte("bench-token")},
	}

	counting := &countingClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(rc, secret).Build(),
		delay:  delay,
	}

	return &ClientManager{
		clients:      []client.Client{counting},
		svcInfoCache: cache.NewLRUExpireCache(svcInfoCacheMaxSize),
	}, counting
}

func BenchmarkGetAuthTokenForRayCluster(b *testing.B) {
	for _, delay := range []time.Duration{0, time.Millisecond, 5 * time.Millisecond} {
		for _, n := range []int{1, 10, 50, 100} {
			b.Run(fmt.Sprintf("delay=%s/concurrency=%d", delay, n), func(b *testing.B) {
				cm, counting := newBenchClientManager(b, delay)
				ctx := context.Background()
				var failures atomic.Int64

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var wg sync.WaitGroup
					wg.Add(n)
					for j := 0; j < n; j++ {
						go func() {
							defer wg.Done()
							if _, err := cm.GetAuthTokenForRayCluster(ctx, benchNamespace, benchClusterName); err != nil {
								failures.Add(1)
							}
						}()
					}
					wg.Wait()
				}
				b.StopTimer()

				if f := failures.Load(); f > 0 {
					b.Fatalf("%d calls failed", f)
				}

				// 2.0 means every caller issued both Gets itself: no coalescing.
				// 2/n means one wave of callers shared a single pair of Gets.
				b.ReportMetric(float64(counting.gets.Load())/float64(b.N*n), "gets/req")
			})
		}
	}
}
