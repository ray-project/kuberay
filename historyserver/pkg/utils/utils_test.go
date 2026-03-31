package utils

import (
	"strings"
	"testing"
	"time"
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
			expectedHexStr: "AQAAAA_==",
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

func TestGetDateTimeFromSessionID(t *testing.T) {
	tests := []struct {
		name         string
		sessionID    string
		expectErr    bool
		expectedTime time.Time
	}{
		{
			name:         "valid session id",
			sessionID:    "session_2024-05-15_10-30-55_123456",
			expectErr:    false,
			expectedTime: time.Date(2024, time.May, 15, 10, 30, 55, 123456000, time.UTC),
		},
		{
			name:      "invalid prefix",
			sessionID: "s_2024-05-15_10-30-55_123456",
			expectErr: true,
		},
		{
			name:      "missing_time",
			sessionID: "session_2024-05-15_10-30-55",
			expectErr: true,
		},
		{
			name:      "empty_string",
			sessionID: "",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTime, err := GetDateTimeFromSessionID(tc.sessionID)

			if tc.expectErr {
				if err == nil {
					t.Errorf("GetDateTimeFromSessionID(%q) succeeded unexpectedly, returned time: %v", tc.sessionID, gotTime)
				}
			} else {
				if err != nil {
					t.Fatalf("GetDateTimeFromSessionID(%q) failed unexpectedly: %v", tc.sessionID, err)
				}
				if !gotTime.Equal(tc.expectedTime) {
					t.Errorf("GetDateTimeFromSessionID(%q) = %v, want %v", tc.sessionID, gotTime, tc.expectedTime)
				}
			}
		})
	}
}

func TestParseMetaFilePath(t *testing.T) {
	tests := []struct {
		name        string
		metaDirPath string
		expectErr   bool
		expected    ClusterInfo
	}{
		{
			name:        "valid raycluster path",
			metaDirPath: "defaultns/raycluster/mycluster1/session_2024-05-15_10-30-55_123456",
			expectErr:   false,
			expected: ClusterInfo{
				Namespace:       "defaultns",
				Name:            "mycluster1",
				SessionName:     "session_2024-05-15_10-30-55_123456",
				CreateTimeStamp: time.Date(2024, time.May, 15, 10, 30, 55, 123456000, time.UTC).Unix(),
				CreateTime:      "2024-05-15T10:30:55Z",
			},
		},
		{
			name:        "valid rayjob path",
			metaDirPath: "defaultns/rayjob/myrayjob/raycluster/mycluster3/session_2024-05-15_10-30-55_123456",
			expectErr:   false,
			expected: ClusterInfo{
				Namespace:       "defaultns",
				OwnerKind:       "rayjob",
				OwnerName:       "myrayjob",
				Name:            "mycluster3",
				SessionName:     "session_2024-05-15_10-30-55_123456",
				CreateTimeStamp: time.Date(2024, time.May, 15, 10, 30, 55, 123456000, time.UTC).Unix(),
				CreateTime:      "2024-05-15T10:30:55Z",
			},
		},
		{
			name:        "valid rayservice path",
			metaDirPath: "defaultns/rayservice/myraysvc/raycluster/mycluster4/session_2024-05-15_10-30-55_123456",
			expectErr:   false,
			expected: ClusterInfo{
				Namespace:       "defaultns",
				OwnerKind:       "rayservice",
				OwnerName:       "myraysvc",
				Name:            "mycluster4",
				SessionName:     "session_2024-05-15_10-30-55_123456",
				CreateTimeStamp: time.Date(2024, time.May, 15, 10, 30, 55, 123456000, time.UTC).Unix(),
				CreateTime:      "2024-05-15T10:30:55Z",
			},
		},
		{
			name:        "invalid length less than 4",
			metaDirPath: "defaultns/raycluster/mycluster1",
			expectErr:   true,
		},
		{
			name:        "invalid length 5",
			metaDirPath: "defaultns/rayjob/myrayjob/mycluster3/session",
			expectErr:   true,
		},
		{
			name:        "invalid kind in length 4",
			metaDirPath: "defaultns/invalidkind/mycluster1/session",
			expectErr:   true,
		},
		{
			name:        "invalid kind in length 6",
			metaDirPath: "defaultns/invalidkind/myrayjob/raycluster/mycluster3/session",
			expectErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseMetaFilePath(tc.metaDirPath)

			if tc.expectErr {
				if err == nil {
					t.Errorf("ParseMetaFilePath(%q) succeeded unexpectedly", tc.metaDirPath)
				}
			} else {
				if err != nil {
					t.Fatalf("ParseMetaFilePath(%q) failed unexpectedly: %v", tc.metaDirPath, err)
				}
				if c.Namespace != tc.expected.Namespace {
					t.Errorf("Namespace = %q, want %q", c.Namespace, tc.expected.Namespace)
				}
				if c.Name != tc.expected.Name {
					t.Errorf("Name = %q, want %q", c.Name, tc.expected.Name)
				}
				if c.SessionName != tc.expected.SessionName {
					t.Errorf("SessionName = %q, want %q", c.SessionName, tc.expected.SessionName)
				}
				if c.OwnerKind != tc.expected.OwnerKind {
					t.Errorf("OwnerKind = %q, want %q", c.OwnerKind, tc.expected.OwnerKind)
				}
				if c.OwnerName != tc.expected.OwnerName {
					t.Errorf("OwnerName = %q, want %q", c.OwnerName, tc.expected.OwnerName)
				}
				if c.CreateTimeStamp != tc.expected.CreateTimeStamp {
					t.Errorf("CreateTimeStamp = %d, want %d", c.CreateTimeStamp, tc.expected.CreateTimeStamp)
				}
				if c.CreateTime != tc.expected.CreateTime {
					t.Errorf("CreateTime = %q, want %q", c.CreateTime, tc.expected.CreateTime)
				}
			}
		})
	}
}
