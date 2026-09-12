package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func TestValidateTopology(t *testing.T) {
	const zone, clique = "topology.kubernetes.io/zone", "nvidia.com/gpu.clique"
	allowed := []string{zone, clique}
	newSpec := func() *rayv1.RayClusterSpec {
		return &rayv1.RayClusterSpec{WorkerGroupSpecs: []rayv1.WorkerGroupSpec{{
			GroupName: "train",
			Template:  corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ray-worker"}}}},
			Topology: &rayv1.TopologySpec{LabelMappings: []rayv1.TopologyLabelMapping{
				{NodeLabel: zone, MapTo: "ray.io/zone"},
				{NodeLabel: clique},
			}},
		}}}
	}

	tests := []struct {
		mutate        func(group *rayv1.WorkerGroupSpec)
		name          string
		errorContains string
	}{
		{name: "valid mappings"},
		{name: "no topology", mutate: func(g *rayv1.WorkerGroupSpec) { g.Topology = nil }},
		{name: "empty labelMappings", mutate: func(g *rayv1.WorkerGroupSpec) { g.Topology.LabelMappings = nil }},
		{
			name: "overwrite-container-cmd annotation",
			mutate: func(g *rayv1.WorkerGroupSpec) {
				g.Template.Annotations = map[string]string{utils.RayOverwriteContainerCmdAnnotationKey: "true"}
			},
			errorContains: utils.RayOverwriteContainerCmdAnnotationKey,
		},
		{
			name: "user command runs ray start",
			mutate: func(g *rayv1.WorkerGroupSpec) {
				g.Template.Spec.Containers[0].Command = []string{"ray start --address=head:6379 --block"}
			},
			errorContains: "runs ray start",
		},
		{
			name: "node label outside the allowlist",
			mutate: func(g *rayv1.WorkerGroupSpec) {
				g.Topology.LabelMappings[1].NodeLabel = "cloud.google.com/gke-nodepool"
			},
			errorContains: `node label "cloud.google.com/gke-nodepool" is not in the operator's allowedNodeLabels`,
		},
		{
			name: "two mappings deliver the same Ray key",
			mutate: func(g *rayv1.WorkerGroupSpec) {
				g.Topology.LabelMappings[1].MapTo = "ray.io/zone"
			},
			errorContains: `Duplicate value: "ray.io/zone"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := newSpec()
			if tt.mutate != nil {
				tt.mutate(&spec.WorkerGroupSpecs[0])
			}
			err := validateTopology(spec, allowed, field.NewPath("spec"))
			if tt.errorContains == "" {
				require.Nil(t, err)
				return
			}
			require.NotNil(t, err)
			require.Contains(t, err.Error(), tt.errorContains)
		})
	}
}
