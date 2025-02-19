package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
)

func TestHasKubectlContext(t *testing.T) {
	sut := &DefaultKubeContexter{}

	tests := []struct {
		config      api.Config
		configFlags *genericclioptions.ConfigFlags
		name        string
		expected    bool
	}{
		{
			name:        "should return false if there's no current context and no --context flag",
			configFlags: &genericclioptions.ConfigFlags{},
			expected:    false,
		},
		{
			name: "should return false if there's no current context and the --context flag is an empty string",
			configFlags: &genericclioptions.ConfigFlags{
				Context: ptr.To(""),
			},
			expected: false,
		},
		{
			name: "should return true if there's no current context but the --context flag is set",
			configFlags: &genericclioptions.ConfigFlags{
				Context: ptr.To("my-context"),
			},
			expected: true,
		},
		{
			name: "should return true if there's a current context but no --context flag",
			config: api.Config{
				CurrentContext: "my-context",
			},
			expected: true,
		},
		{
			name: "should return true if there's a current context and the --context flag is set",
			config: api.Config{
				CurrentContext: "my-context",
			},
			configFlags: &genericclioptions.ConfigFlags{
				Context: ptr.To("my-context"),
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, sut.HasContext(tc.config, tc.configFlags))
		})
	}
}
