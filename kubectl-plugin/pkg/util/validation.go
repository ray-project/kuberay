package util

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

func ValidateResourceQuantity(value string, name string) error {
	if value == "" {
		return nil
	}

	q, err := resource.ParseQuantity(value)
	if err != nil {
		return fmt.Errorf("%s is not a valid resource quantity: %w", name, err)
	}
	if q.Sign() < 0 {
		return fmt.Errorf("%s cannot be negative", name)
	}
	return nil
}

func ValidateTPUNodeSelector(numOfHosts int32, nodeSelector map[string]string) error {
	// @TODO:
	// In the future we could validate that the accelerator and topology nodeSelectors are supported values,
	// and also validate the value for numOfHosts since it is deterministic based on the previous two values.
	// https://github.com/ray-project/kuberay/pull/3258#discussion_r2027973436

	const docURL = "https://cloud.google.com/kubernetes-engine/docs/concepts/plan-tpus#availability"

	if numOfHosts == 0 {
		return fmt.Errorf("numOfHosts cannot be 0 when using TPU")
	}
	if _, ok := nodeSelector[NodeSelectorGKETPUAccelerator]; !ok {
		return fmt.Errorf("%s is not set in --worker-node-selectors. See %s for supported values", NodeSelectorGKETPUAccelerator, docURL)
	}
	if _, ok := nodeSelector[NodeSelectorGKETPUTopology]; !ok {
		return fmt.Errorf("%s is not set in --worker-node-selectors. See %s for supported values", NodeSelectorGKETPUTopology, docURL)
	}
	return nil
}
