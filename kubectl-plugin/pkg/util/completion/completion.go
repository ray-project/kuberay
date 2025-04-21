package completion

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/completion"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
)

// RayResourceTypeCompletionFunc Returns a completion function that completes the Ray resource type.
// That is, raycluster, rayjob, or rayservice.
func RayResourceTypeCompletionFunc() func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var comps []string
		directive := cobra.ShellCompDirectiveNoFileComp
		resourceTypes := getAllRayResourceType()
		for _, resourceType := range resourceTypes {
			if strings.HasPrefix(resourceType, toComplete) {
				comps = append(comps, resourceType)
			}
		}
		return comps, directive
	}
}

// RayClusterCompletionFunc Returns a completion function that completes RayCluster resource names.
func RayClusterCompletionFunc(f cmdutil.Factory) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return completion.ResourceNameCompletionFunc(f, string(util.RayCluster))
}

// RayJobCompletionFunc Returns a completion function that completes RayJob resource names.
func RayJobCompletionFunc(f cmdutil.Factory) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return completion.ResourceNameCompletionFunc(f, string(util.RayJob))
}

// RayServiceCompletionFunc Returns a completion function that completes RayService resource names.
func RayServiceCompletionFunc(f cmdutil.Factory) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return completion.ResourceNameCompletionFunc(f, string(util.RayService))
}

// RayClusterResourceNameCompletionFunc Returns completions of:
// 1- RayCluster names that match the toComplete prefix
// 2- Ray resource types which match the toComplete prefix
func RayClusterResourceNameCompletionFunc(f cmdutil.Factory) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var comps []string
		directive := cobra.ShellCompDirectiveNoFileComp
		if len(args) == 0 {
			comps, directive = doRayClusterCompletion(f, toComplete)
		}
		return comps, directive
	}
}

func getAllRayResourceType() []string {
	return []string{
		string(util.RayCluster),
		string(util.RayJob),
		string(util.RayService),
	}
}

// doRayClusterCompletion Returns completions of:
// 1- RayCluster names that match the toComplete prefix
// 2- Ray resource types which match the toComplete prefix
// Ref: https://github.com/kubernetes/kubectl/blob/262825a8a665c7cae467dfaa42b63be5a5b8e5a2/pkg/util/completion/completion.go#L434
func doRayClusterCompletion(f cmdutil.Factory, toComplete string) ([]string, cobra.ShellCompDirective) {
	var comps []string
	directive := cobra.ShellCompDirectiveNoFileComp
	slashIdx := strings.Index(toComplete, "/")
	if slashIdx == -1 {
		// Standard case, complete RayCluster names
		comps = completion.CompGetResource(f, string(util.RayCluster), toComplete)

		// Also include resource choices for the <type>/<name> form
		resourceTypes := getAllRayResourceType()

		if len(comps) == 0 {
			// If there are no RayCluster to complete, we will only be completing
			// <type>/.  We should disable adding a space after the /.
			directive |= cobra.ShellCompDirectiveNoSpace
		}

		for _, resource := range resourceTypes {
			if strings.HasPrefix(resource, toComplete) {
				comps = append(comps, fmt.Sprintf("%s/", resource))
			}
		}
	} else {
		// Dealing with the <type>/<name> form, use the specified resource type
		resourceType := toComplete[:slashIdx]
		toComplete = toComplete[slashIdx+1:]
		nameComps := completion.CompGetResource(f, resourceType, toComplete)
		for _, c := range nameComps {
			comps = append(comps, fmt.Sprintf("%s/%s", resourceType, c))
		}
	}
	return comps, directive
}
