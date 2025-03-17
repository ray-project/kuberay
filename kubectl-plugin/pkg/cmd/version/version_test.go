package version

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	clientfake "github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// Tests the Run() step of the command and checks the output.
func TestRayVersionRun(t *testing.T) {
	fakeVersionOptions := &VersionOptions{
		cmdFactory: cmdutil.NewFactory(genericclioptions.NewConfigFlags(true)),
	}

	tests := []struct {
		name                           string
		kuberayImageVersion            string
		getKubeRayOperatorVersionError error
		expected                       string
	}{
		{
			name:                           "Test when we can successfully get the KubeRay operator image version",
			kuberayImageVersion:            "v0.0.1",
			getKubeRayOperatorVersionError: nil,
			expected: fmt.Sprintf("kubectl ray plugin version: %s\n"+
				"KubeRay operator version: %s\n", Version, "v0.0.1"),
		},
		{
			name:                           "Test when we cannot get the KubeRay operator image version",
			kuberayImageVersion:            "",
			getKubeRayOperatorVersionError: fmt.Errorf("something went wrong"),
			expected: fmt.Sprintf("kubectl ray plugin version: %s\n"+
				"warning: KubeRay operator installation cannot be found: something went wrong. "+
				"Did you install it with the name \"kuberay-operator\"?\n", Version),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := clientfake.NewFakeClient().WithKubeRayImageVersion(tc.kuberayImageVersion).WithKubeRayOperatorVersionError(tc.getKubeRayOperatorVersionError)

			var buf bytes.Buffer
			err := fakeVersionOptions.Run(context.Background(), client, &buf)
			require.NoError(t, err)

			assert.Equal(t, tc.expected, buf.String())
		})
	}
}
