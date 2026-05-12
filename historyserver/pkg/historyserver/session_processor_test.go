package historyserver

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

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

func rayCluster(namespace, name, headSvcName string) *rayv1.RayCluster {
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Status: rayv1.RayClusterStatus{
			Head: rayv1.HeadInfo{ServiceName: headSvcName},
		},
	}
}

type fakeResolver struct {
	sessionName string
	err         error
	called      bool
}

func (f *fakeResolver) FetchSessionName(_ context.Context, _, _ string) (string, error) {
	f.called = true
	return f.sessionName, f.err
}

func TestIsDead(t *testing.T) {
	const (
		ns             = "default"
		name           = "raycluster-test"
		headSvc        = "raycluster-test-head-svc"
		queriedSession = "session_2026-04-22_10-00-00_000000_1"
	)

	tests := []struct {
		name         string
		cr           *rayv1.RayCluster // nil = RayCluster CR absent
		resolverSess string
		resolverErr  error
		wantDead     bool
		wantErr      bool
		wantCalled   bool
	}{
		{
			name:       "RayCluster CR absent -> dead",
			cr:         nil,
			wantDead:   true,
			wantCalled: false,
		},
		{
			name:       "head service not ready -> error",
			cr:         rayCluster(ns, name, ""),
			wantErr:    true,
			wantCalled: false,
		},
		{
			name:        "resolver returns error -> error",
			cr:          rayCluster(ns, name, headSvc),
			resolverErr: errors.New("probe failed"),
			wantErr:     true,
			wantCalled:  true,
		},
		{
			name:         "live session matches queried session -> live",
			cr:           rayCluster(ns, name, headSvc),
			resolverSess: queriedSession,
			wantDead:     false,
			wantCalled:   true,
		},
		{
			name:         "live session differs from queried session -> dead",
			cr:           rayCluster(ns, name, headSvc),
			resolverSess: "session_other_xxx",
			wantDead:     true,
			wantCalled:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := newFakeK8sClient(t, tc.cr)
			r := &fakeResolver{sessionName: tc.resolverSess, err: tc.resolverErr}
			p := NewSessionProcessor(nil, k, r)

			info := utils.ClusterInfo{Namespace: ns, Name: name, SessionName: queriedSession}
			gotDead, err := p.isDead(context.Background(), info)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if gotDead != tc.wantDead {
					t.Fatalf("isDead = %v, want %v", gotDead, tc.wantDead)
				}
			}

			if r.called != tc.wantCalled {
				t.Fatalf("resolver called = %v, want %v", r.called, tc.wantCalled)
			}
		})
	}
}
