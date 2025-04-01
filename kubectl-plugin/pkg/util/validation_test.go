package util

import (
	"fmt"
	"testing"
)

func TestValidateResourceQuantity(t *testing.T) {
	tests := []struct {
		value   string
		name    string
		wantErr bool
	}{
		{"500m", "cpu", false},
		{"-500m", "cpu", true},
		{"aaa", "cpu", true},
		{"10Gi", "memory", false},
		{"bbb", "memory", true},
		{"", "memory", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateResourceQuantity(tt.value, tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateResourceQuantity() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTPUNodeSelector(t *testing.T) {
	tests := []struct {
		nodeSelector map[string]string
		wantErr      bool
	}{
		{map[string]string{}, true},
		{map[string]string{NodeSelectorGKETPUAccelerator: "v2"}, true},
		{map[string]string{NodeSelectorGKETPUTopology: "topology-1"}, true},
		{map[string]string{NodeSelectorGKETPUAccelerator: "v2", NodeSelectorGKETPUTopology: "topology-1"}, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.nodeSelector), func(t *testing.T) {
			err := ValidateTPUNodeSelector(tt.nodeSelector)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTPUNodeSelector() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
