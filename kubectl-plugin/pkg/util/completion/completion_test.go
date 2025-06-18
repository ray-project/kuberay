package completion

import (
	"sort"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRayResourceTypeCompletionFunc(t *testing.T) {
	compFunc := RayResourceTypeCompletionFunc()
	comps, directive := compFunc(nil, []string{}, "")
	checkCompletion(t, comps, []string{"raycluster", "rayjob", "rayservice"}, directive, cobra.ShellCompDirectiveNoFileComp)
}

func checkCompletion(t *testing.T, comps, expectedComps []string, directive, expectedDirective cobra.ShellCompDirective) {
	if e, d := expectedDirective, directive; e != d {
		t.Errorf("expected directive\n%v\nbut got\n%v", e, d)
	}

	sort.Strings(comps)
	sort.Strings(expectedComps)

	require.ElementsMatch(t, expectedComps, comps)
}
