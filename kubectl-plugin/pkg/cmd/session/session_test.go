package session

import (
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestComplete(t *testing.T) {
	cmd := &cobra.Command{Use: "session"}

	tests := []struct {
		name                 string
		namespace            string
		expectedResourceType util.ResourceType
		expectedNamespace    string
		expectedName         string
		args                 []string
		hasErr               bool
	}{
		{
			name:                 "valid raycluster without namespace",
			namespace:            "",
			args:                 []string{"raycluster/test-raycluster"},
			expectedResourceType: util.RayCluster,
			expectedNamespace:    "default",
			expectedName:         "test-raycluster",
			hasErr:               false,
		},
		{
			name:                 "valid RayCluster with namespace",
			namespace:            "test-namespace",
			args:                 []string{"raycluster/test-raycluster"},
			expectedResourceType: util.RayCluster,
			expectedNamespace:    "test-namespace",
			expectedName:         "test-raycluster",
			hasErr:               false,
		},
		{
			name:                 "valid RayJob without namespace",
			namespace:            "",
			args:                 []string{"rayjob/test-rayjob"},
			expectedResourceType: util.RayJob,
			expectedNamespace:    "default",
			expectedName:         "test-rayjob",
			hasErr:               false,
		},
		{
			name:                 "valid RayService without namespace",
			namespace:            "",
			args:                 []string{"rayservice/test-rayservice"},
			expectedResourceType: util.RayService,
			expectedNamespace:    "default",
			expectedName:         "test-rayservice",
			hasErr:               false,
		},
		{
			name:                 "no slash default to raycluster",
			namespace:            "",
			args:                 []string{"test-resource"},
			expectedResourceType: util.RayCluster,
			expectedNamespace:    "default",
			expectedName:         "test-resource",
			hasErr:               false,
		},
		{
			name:   "invalid args (no args)",
			args:   []string{},
			hasErr: true,
		},
		{
			name:   "invalid args (too many args)",
			args:   []string{"raycluster/test-raycluster", "extra-arg"},
			hasErr: true,
		},
		{
			name:   "invalid args (no resource type)",
			args:   []string{"/test-resource"},
			hasErr: true,
		},
		{
			name:   "invalid args (no resource name)",
			args:   []string{"raycluster/"},
			hasErr: true,
		},
		{
			name:   "invalid args (invalid resource type)",
			args:   []string{"invalid-type/test-resource"},
			hasErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testStreams, _, _, _ := genericiooptions.NewTestIOStreams()
			fakeSessionOptions := NewSessionOptions(testStreams)
			fakeSessionOptions.configFlags.Namespace = &tc.namespace
			err := fakeSessionOptions.Complete(cmd, tc.args)
			if tc.hasErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedNamespace, fakeSessionOptions.Namespace)
				assert.Equal(t, tc.expectedResourceType, fakeSessionOptions.ResourceType)
				assert.Equal(t, tc.expectedName, fakeSessionOptions.ResourceName)
			}
		})
	}
}
