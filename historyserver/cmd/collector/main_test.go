package main

import "testing"

func TestResolveDashboardAddress(t *testing.T) {
	tests := []struct {
		name      string
		flagValue string
		envValue  string
		want      string
	}{
		{
			name:      "built-in default",
			flagValue: defaultDashboardAddress,
			want:      defaultDashboardAddress,
		},
		{
			name:      "flag only",
			flagValue: "http://flag:8265",
			want:      "http://flag:8265",
		},
		{
			name:      "environment only",
			flagValue: defaultDashboardAddress,
			envValue:  "http://env:8265",
			want:      "http://env:8265",
		},
		{
			name:      "environment overrides flag",
			flagValue: "http://flag:8265",
			envValue:  "http://env:8265",
			want:      "http://env:8265",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("RAY_DASHBOARD_ADDRESS", test.envValue)
			if got := resolveDashboardAddress(test.flagValue); got != test.want {
				t.Fatalf("resolveDashboardAddress(%q) = %q, want %q", test.flagValue, got, test.want)
			}
		})
	}
}
