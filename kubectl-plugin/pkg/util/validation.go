package util

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

const tpuDocURL = "https://cloud.google.com/kubernetes-engine/docs/concepts/plan-tpus#availability"

func ValidateResourceQuantity(value string, name string) error {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return fmt.Errorf("%s is not a valid resource quantity: %w", name, err)
	}
	if q.Sign() < 0 {
		return fmt.Errorf("%s cannot be negative", name)
	}
	return nil
}

func ValidateTPU(tpu *string, numOfHosts *int32, nodeSelector map[string]string) error {
	if tpu == nil || *tpu == "" || *tpu == "0" {
		return nil
	}

	if numOfHosts != nil && *numOfHosts == 0 {
		return fmt.Errorf("numOfHosts cannot be 0 when using TPU")
	}
	accelerator, ok := nodeSelector[NodeSelectorGKETPUAccelerator]
	if !ok {
		return fmt.Errorf("%s is not set in --worker-node-selectors. See %s for supported values", NodeSelectorGKETPUAccelerator, tpuDocURL)
	}
	topology, ok := nodeSelector[NodeSelectorGKETPUTopology]
	if !ok {
		return fmt.Errorf("%s is not set in --worker-node-selectors. See %s for supported values", NodeSelectorGKETPUTopology, tpuDocURL)
	}

	if err := ValidateResourceQuantity(*tpu, "TPU"); err != nil {
		return err
	}
	tpuQuantity := resource.MustParse(*tpu)
	tpuPerHost := tpuQuantity.Value()
	if tpuPerHost <= 0 {
		return fmt.Errorf("TPU must be greater than 0")
	}

	spec, ok := supportedTPUAccelerators[accelerator]
	if !ok {
		return fmt.Errorf("unsupported TPU accelerator %q. See %s for supported values", accelerator, tpuDocURL)
	}

	allowedTPUPerHost, err := spec.allowedTPUPerHost(topology)
	if err != nil {
		return err
	}
	if !slices.Contains(allowedTPUPerHost, tpuPerHost) {
		return fmt.Errorf("%d TPUs per host is not valid for accelerator %q with topology %q. See %s for supported values", tpuPerHost, accelerator, topology, tpuDocURL)
	}

	dims, err := parseTPUTopology(topology)
	if err != nil {
		return fmt.Errorf("invalid TPU topology %q: %w. See %s for supported values", topology, err, tpuDocURL)
	}
	totalTPUs := 1
	for _, d := range dims {
		totalTPUs *= d
	}
	if int64(totalTPUs)%tpuPerHost != 0 {
		return fmt.Errorf("TPU topology %q has %d TPUs, which is not divisible by %d TPUs per host. See %s", topology, totalTPUs, tpuPerHost, tpuDocURL)
	}

	expectedNumOfHosts := int32(int64(totalTPUs) / tpuPerHost)
	actualNumOfHosts := int32(DefaultNumOfHosts)
	if numOfHosts != nil {
		actualNumOfHosts = *numOfHosts
	}
	if actualNumOfHosts != expectedNumOfHosts {
		return fmt.Errorf("numOfHosts must be %d for accelerator %q with topology %q and %d TPUs per host, got %d. See %s", expectedNumOfHosts, accelerator, topology, tpuPerHost, actualNumOfHosts, tpuDocURL)
	}
	return nil
}

// supportedTPUAccelerators lists GKE TPU accelerator values and topologies from
// https://cloud.google.com/kubernetes-engine/docs/concepts/plan-tpus#availability
//
// TPU 8t/8i are not yet listed in the GKE TPU availability table; add them when GKE publishes the values.
var supportedTPUAccelerators = map[string]tpuAccelerator{
	// Ironwood (TPU7x): 4 TPUs per host, 3D topologies.
	"tpu7x": {
		is3D:       true,
		tpuPerHost: 4,
		maxTPUs:    16 * 16 * 36,
	},
	// TPU Trillium (v6e).
	"tpu-v6e-slice": {
		topologies: map[string][]int64{
			"1x1":   {1},
			"2x2":   {4},
			"2x4":   {4, 8},
			"4x4":   {4},
			"4x8":   {4},
			"8x8":   {4},
			"8x16":  {4},
			"16x16": {4},
		},
	},
	// TPU v5p: 4 TPUs per host, 3D topologies.
	"tpu-v5p-slice": {
		is3D:       true,
		tpuPerHost: 4,
		maxTPUs:    16 * 16 * 24,
	},
	// TPU v5e.
	"tpu-v5-lite-podslice": {
		topologies: map[string][]int64{
			"1x1":   {1},
			"2x2":   {4},
			"2x4":   {4, 8},
			"4x4":   {4},
			"4x8":   {4},
			"8x8":   {4},
			"8x16":  {4},
			"16x16": {4},
		},
	},
	// TPU v4: 4 TPUs per host, 3D topologies. Largest documented topology is 12x16x16.
	"tpu-v4-podslice": {
		is3D:       true,
		tpuPerHost: 4,
		maxTPUs:    12 * 16 * 16,
	},
	// TPU v3 pod slice. Standard machine types use 4 TPUs/host; Autopilot docs also list 8.
	"tpu-v3-slice": {
		topologies: map[string][]int64{
			"4x4":   {4, 8},
			"4x8":   {4, 8},
			"8x8":   {4, 8},
			"8x16":  {4, 8},
			"16x16": {4, 8},
			"16x32": {4},
			"32x32": {4},
		},
	},
	// TPU v3 single-host device.
	"tpu-v3-device": {
		topologies: map[string][]int64{
			"2x2": {4},
		},
	},
}

type tpuAccelerator struct {
	// topologies maps a 2D topology (e.g. "4x4") to allowed TPU counts per host.
	topologies map[string][]int64
	// is3D indicates AxBxC topologies with a fixed TPU count per host.
	is3D       bool
	tpuPerHost int64
	maxTPUs    int
}

func (a tpuAccelerator) allowedTPUPerHost(topology string) ([]int64, error) {
	if a.topologies != nil {
		tpuPerHost, ok := a.topologies[topology]
		if !ok {
			return nil, fmt.Errorf("unsupported TPU topology %q. See %s for supported values", topology, tpuDocURL)
		}
		return tpuPerHost, nil
	}
	if !a.is3D {
		return nil, fmt.Errorf("internal error: TPU accelerator has neither 2D nor 3D topologies")
	}
	dims, err := parseTPUTopology(topology)
	if err != nil {
		return nil, fmt.Errorf("invalid TPU topology %q: %w. See %s for supported values", topology, err, tpuDocURL)
	}
	if len(dims) != 3 {
		return nil, fmt.Errorf("accelerator requires a 3D topology (e.g. 2x2x2), got %q. See %s for supported values", topology, tpuDocURL)
	}
	if !isValid3DTopology(dims, a.maxTPUs) {
		return nil, fmt.Errorf("unsupported TPU topology %q. See %s for supported values", topology, tpuDocURL)
	}
	return []int64{a.tpuPerHost}, nil
}

func parseTPUTopology(topology string) ([]int, error) {
	parts := strings.Split(topology, "x")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, fmt.Errorf("must be 2D (e.g. 2x4) or 3D (e.g. 2x2x2)")
	}
	dims := make([]int, len(parts))
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid dimension %q", part)
		}
		dims[i] = n
	}
	return dims, nil
}

// isValid3DTopology reports whether dims is a supported AxBxC TPU topology:
//   - 2x2x1 is the documented single-host 4-TPU topology
//   - topologies with <= 64 TPUs: each dimension is a multiple of 2
//   - topologies with > 64 TPUs: each dimension is a multiple of 4 and A <= B <= C
func isValid3DTopology(dims []int, maxTPUs int) bool {
	if len(dims) != 3 {
		return false
	}
	a, b, c := dims[0], dims[1], dims[2]
	totalTPUs := a * b * c
	if totalTPUs <= 0 || totalTPUs > maxTPUs {
		return false
	}
	if a == 2 && b == 2 && c == 1 {
		return true
	}
	if totalTPUs <= 64 {
		return a%2 == 0 && b%2 == 0 && c%2 == 0
	}
	return a%4 == 0 && b%4 == 0 && c%4 == 0 && a <= b && b <= c
}
