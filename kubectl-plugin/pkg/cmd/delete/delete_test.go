package kubectlraydelete

import (
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestComplete(t *testing.T) {
	cmd := &cobra.Command{Use: "deleete"}

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
			name:                 "valid raycluster without explicit resource and without namespace",
			namespace:            "",
			expectedResourceType: util.RayCluster,
			expectedNamespace:    "default",
			expectedName:         "test-raycluster",
			args:                 []string{"test-raycluster"},
			hasErr:               false,
		},
		{
			name:                 "valid raycluster with explicit resource and with namespace",
			namespace:            "test-namespace",
			expectedResourceType: util.RayCluster,
			expectedNamespace:    "test-namespace",
			expectedName:         "test-raycluster",
			args:                 []string{"raycluster/test-raycluster"},
			hasErr:               false,
		},
		{
			name:                 "valid raycluster without explicit resource and with namespace",
			namespace:            "test-namespace",
			expectedResourceType: util.RayCluster,
			expectedNamespace:    "test-namespace",
			expectedName:         "test-raycluster",
			args:                 []string{"test-raycluster"},
			hasErr:               false,
		},
		{
			name:                 "valid rayjob with namespace",
			namespace:            "test-namespace",
			expectedResourceType: util.RayJob,
			expectedNamespace:    "test-namespace",
			expectedName:         "test-rayjob",
			args:                 []string{"rayjob/test-rayjob"},
			hasErr:               false,
		},
		{
			name:                 "valid rayservice with namespace",
			namespace:            "test-namespace",
			expectedResourceType: util.RayService,
			expectedNamespace:    "test-namespace",
			expectedName:         "test-rayservice",
			args:                 []string{"rayservice/test-rayservice"},
			hasErr:               false,
		},
		{
			name:      "invalid service type",
			namespace: "test-namespace",
			args:      []string{"rayserve/test-rayserve"},
			hasErr:    true,
		},
		{
			name:                 "valid raycluster with namespace but weird ray type casing",
			namespace:            "test-namespace",
			expectedResourceType: util.RayCluster,
			expectedNamespace:    "test-namespace",
			expectedName:         "test-raycluster",
			args:                 []string{"rayCluStER/test-raycluster"},
			hasErr:               false,
		},
		{
			name:   "invalid args, too many args",
			args:   []string{"test", "raytype", "raytypename"},
			hasErr: true,
		},
		{
			name:   "invalid args, non valid resource type",
			args:   []string{"test/test"},
			hasErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
			fakeDeleteOptions := NewDeleteOptions(testStreams)
			fakeDeleteOptions.configFlags.Namespace = &tc.namespace
			err := fakeDeleteOptions.Complete(cmd, tc.args)
			if tc.hasErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tc.expectedName, fakeDeleteOptions.ResourceName)
				assert.Equal(t, tc.expectedNamespace, fakeDeleteOptions.Namespace)
				assert.Equal(t, tc.expectedResourceType, fakeDeleteOptions.ResourceType)
			}
		})
	}
}
