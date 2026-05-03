package historyserver

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// newFakeK8sClient builds a controller-runtime fake client preloaded with the
// given RayCluster, or none when rc is nil.
func newFakeK8sClient(t *testing.T, rc *rayv1.RayCluster) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(rayv1.AddToScheme(scheme))
	b := fake.NewClientBuilder().WithScheme(scheme)
	if rc != nil {
		b = b.WithObjects(rc)
	}
	return b.Build()
}

// rayCluster constructs a RayCluster CR with the given namespace/name and
// creation timestamp.
func rayCluster(namespace, name string, createdAt time.Time) *rayv1.RayCluster {
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			CreationTimestamp: metav1.NewTime(createdAt),
		},
	}
}

// TestIsDead exercises the dead-detection contract: a session is dead if the
// CR is absent or if it predates the current CR's creation timestamp (the
// recreated-same-name case). Defensive fallback for unparseable session names
// keeps the legacy "CR exists = live" behavior.
func TestIsDead(t *testing.T) {
	const ns, name = "default", "raycluster-test"
	crCreatedAt := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)

	sessionBeforeCR := "session_2026-04-22_09-00-00_000000_1" // 1h before CR
	sessionAfterCR := "session_2026-04-22_11-00-00_000000_1"  // 1h after CR

	tests := []struct {
		name        string
		cr          *rayv1.RayCluster // nil = RayCluster CR absent
		sessionName string
		wantDead    bool
	}{
		{
			name:        "RayCluster CR absent -> dead",
			cr:          nil,
			sessionName: sessionAfterCR,
			wantDead:    true,
		},
		{
			name:        "RayCluster CR exists, session predates CR (recreated-same-name) -> dead",
			cr:          rayCluster(ns, name, crCreatedAt),
			sessionName: sessionBeforeCR,
			wantDead:    true,
		},
		{
			name:        "RayCluster CR exists, session created after CR -> live",
			cr:          rayCluster(ns, name, crCreatedAt),
			sessionName: sessionAfterCR,
			wantDead:    false,
		},
		{
			name:        "unparseable session name -> live",
			cr:          rayCluster(ns, name, crCreatedAt),
			sessionName: "not-a-session-name",
			wantDead:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := newFakeK8sClient(t, tc.cr)
			p := NewPipeline(nil, k)

			info := utils.ClusterInfo{Namespace: ns, Name: name, SessionName: tc.sessionName}
			gotDead, err := p.isDead(context.Background(), info)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotDead != tc.wantDead {
				t.Fatalf("isDead = %v, want %v", gotDead, tc.wantDead)
			}
		})
	}
}
