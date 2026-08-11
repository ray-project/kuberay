package kuberneteswas

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
)

type fakeProvider struct {
	gv        schema.GroupVersion
	available error
}

func (f *fakeProvider) GroupVersion() schema.GroupVersion                            { return f.gv }
func (f *fakeProvider) Available(*rest.Config) error                                 { return f.available }
func (f *fakeProvider) AddToScheme(*runtime.Scheme)                                  {}
func (f *fakeProvider) ConfigureReconciler(b *builder.Builder) *builder.Builder      { return b }
func (f *fakeProvider) NewScheduler(client.Client) schedulerinterface.BatchScheduler { return nil }

// withProviders swaps the package registry for the duration of a test.
func withProviders(t *testing.T, providers ...Provider) {
	t.Helper()
	original := registeredProviders
	registeredProviders = providers
	t.Cleanup(func() { registeredProviders = original })
}

func TestSelectProviderNoneRegistered(t *testing.T) {
	withProviders(t)

	_, err := selectProvider(&rest.Config{})
	require.Error(t, err)
}

func TestSelectProviderNilConfigReturnsFirst(t *testing.T) {
	first := &fakeProvider{gv: schema.GroupVersion{Group: "scheduling.k8s.io", Version: "v1alpha2"}}
	second := &fakeProvider{gv: schema.GroupVersion{Group: "scheduling.k8s.io", Version: "v1alpha3"}}
	withProviders(t, first, second)

	got, err := selectProvider(nil)
	require.NoError(t, err)
	require.Same(t, first, got)
}

func TestSelectProviderReturnsFirstAvailable(t *testing.T) {
	unavailable := &fakeProvider{gv: schema.GroupVersion{Version: "v1alpha3"}, available: errors.New("not served")}
	available := &fakeProvider{gv: schema.GroupVersion{Version: "v1alpha2"}}
	withProviders(t, unavailable, available)

	got, err := selectProvider(&rest.Config{})
	require.NoError(t, err)
	require.Same(t, available, got)
}

func TestSelectProviderAllUnavailable(t *testing.T) {
	withProviders(t, &fakeProvider{gv: schema.GroupVersion{Version: "v1alpha2"}, available: errors.New("not served")})

	_, err := selectProvider(&rest.Config{})
	require.Error(t, err)
}
