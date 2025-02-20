package util

import (
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
