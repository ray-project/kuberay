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
	if numOfHosts == 0 {
		return fmt.Errorf("numOfHosts cannot be 0 when using TPU")
	}
	if _, ok := nodeSelector[NodeSelectorGKETPUAccelerator]; !ok {
		return fmt.Errorf("%s is not set in --worker-node-selectors", NodeSelectorGKETPUAccelerator)
	}
	if _, ok := nodeSelector[NodeSelectorGKETPUTopology]; !ok {
		return fmt.Errorf("%s is not set in --worker-node-selectors", NodeSelectorGKETPUTopology)
	}
	return nil
}
