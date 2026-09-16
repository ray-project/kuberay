package v1

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// validateTopology checks the topology field of every worker group in spec against the operator allowlist.
// annotations are the admitted object's metadata annotations, base is the field path of spec in that object
func validateTopology(spec *rayv1.RayClusterSpec, annotations map[string]string, allowed []string, base *field.Path) *field.Error {
	for i := range spec.WorkerGroupSpecs {
		group := &spec.WorkerGroupSpecs[i]
		if group.Topology == nil || len(group.Topology.LabelMappings) == 0 {
			continue
		}
		path := base.Child("workerGroupSpecs").Index(i).Child("topology")

		// node label delivery rewrites the KubeRay-generated ray start command, so the user must not replace it.
		// the annotation is read from the CR metadata and copied onto the pod template, so check both
		if overwritesContainerCmd(annotations) || overwritesContainerCmd(group.Template.Annotations) {
			return field.Forbidden(path, fmt.Sprintf("cannot be combined with the %s annotation; node label delivery needs the KubeRay-generated ray start command", utils.RayOverwriteContainerCmdAnnotationKey))
		}
		if len(group.Template.Spec.Containers) > utils.RayContainerIndex {
			container := group.Template.Spec.Containers[utils.RayContainerIndex]
			if strings.Contains(strings.Join(container.Command, " ")+" "+strings.Join(container.Args, " "), "ray start") {
				return field.Forbidden(path, "cannot be combined with a container command that runs ray start; node label delivery needs the KubeRay-generated ray start command")
			}
		}
		// check for duplicate label keys
		seen := make(map[string]bool, len(group.Topology.LabelMappings))
		for j, mapping := range group.Topology.LabelMappings {
			mappingPath := path.Child("labelMappings").Index(j)
			if !slices.Contains(allowed, mapping.NodeLabel) {
				return field.Forbidden(mappingPath.Child("nodeLabel"), fmt.Sprintf("node label %q is not in the operator's allowedNodeLabels", mapping.NodeLabel))
			}
			// mapTo defaults to nodeLabel
			rayKey := mapping.MapTo
			if rayKey == "" {
				rayKey = mapping.NodeLabel
			}
			if seen[rayKey] {
				return field.Duplicate(mappingPath, rayKey)
			}
			seen[rayKey] = true
		}
	}
	return nil
}

func overwritesContainerCmd(annotations map[string]string) bool {
	v, ok := annotations[utils.RayOverwriteContainerCmdAnnotationKey]
	return ok && strings.ToLower(v) == "true"
}
