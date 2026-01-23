package utils

import (
	"strings"
	"testing"
)

func TestSlice(t *testing.T) {
	strs := []string{"1", "2", "3", "4", "5"}
	t.Logf("laster %s", strs[len(strs)-1])
	t.Logf("not laster all %s", strings.Join(strs[:len(strs)-1], ""))
	clusterNameId := "a_s_sdf_sdfsdfsdfisfdf1_2safd-0sdf-sdf-000"
	strs = strings.Split(clusterNameId, "_")
	t.Logf("laster %s", strs[len(strs)-1])
	t.Logf("not laster all %s", strings.Join(strs[:len(strs)-1], "_"))

	clusterNameId = "a-s-sdf-sdfsdfsdfisfdf1A_B2safd-0sdf-sdf-000"
	strs = strings.Split(clusterNameId, "_")
	t.Logf("laster %s", strs[len(strs)-1])
	t.Logf("not laster all %s", strings.Join(strs[:len(strs)-1], "_"))
}

func TestConvertBase64ToHex(t *testing.T) {
	//TODO(chiayi): use real base64 strings from job ids as tests ex: AgAAAA==
	tests := []struct {
		scenario       string
		base64Str      string
		expectedHexStr string
		expectError    bool
	}{
		{
			scenario:       "Successful convertion from base64 to hex",
			base64Str:      "AgAAAA==",
			expectedHexStr: "02000000",
			expectError:    false,
		},
		{
			scenario:       "Failed convertion from base64 to hex - contains '_'",
			base64Str:      "AQAAAA_==",
			expectedHexStr: "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			actualHexStr, err := ConvertBase64ToHex(tt.base64Str)
			if err == nil && tt.expectError {
				t.Errorf("ConvertToBase64ToHex() expected error for base64 string %s", tt.base64Str)
			}
			if actualHexStr != tt.expectedHexStr {
				t.Errorf("Actual convertion does not match expected result. Actual: %s Expected: %s", actualHexStr, tt.expectedHexStr)
			}
		})
	}
}
