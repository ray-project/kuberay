package utils

import (
	"context"
	"strings"
	"testing"
	"time"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func TestGetRayClusterOwnerInfoFromClient(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(rayv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	clusterName := "test-cluster"
	namespace := "test-ns"
	trueVal := true

	t.Setenv(RAYCLUSTER_NAME_ENV_VAR, clusterName)

	tests := []struct {
		name          string
		rayCluster    *rayv1.RayCluster
		expectedKind  string
		expectedName  string
		expectedError bool
	}{
		{
			name: "with controller owner reference",
			rayCluster: &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "RayJob",
							Name:       "my-job",
							Controller: &trueVal,
						},
					},
				},
			},
			expectedKind: "RayJob",
			expectedName: "my-job",
		},
		{
			name: "with multiple owner references, fallback to first",
			rayCluster: &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "RayService", Name: "my-service"},
						{Kind: "RandomReference", Name: "random-reference"},
					},
				},
			},
			expectedKind: "RayService",
			expectedName: "my-service",
		},
		{
			name: "no owner references",
			rayCluster: &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			},
			expectedError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.rayCluster).Build()
			kind, name, err := getRayClusterOwnerInfoFromClient(context.Background(), fakeClient, namespace)

			if tc.expectedError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind != tc.expectedKind || name != tc.expectedName {
				t.Errorf("got (%s, %s), want (%s, %s)", kind, name, tc.expectedKind, tc.expectedName)
			}
		})
	}
}
