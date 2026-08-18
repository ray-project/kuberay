package tls

import (
	"crypto/tls"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestParseProfile(t *testing.T) {
	tests := []struct {
		profile        *configv1.TLSSecurityProfile
		name           string
		wantCiphers    []uint16
		wantMinVersion uint16
	}{
		{
			name:           "nil profile returns Intermediate defaults",
			profile:        nil,
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name:           "empty profile returns Intermediate defaults",
			profile:        &configv1.TLSSecurityProfile{},
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name: "Intermediate type",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name: "Modern returns TLS 1.3 with nil ciphers",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
			wantMinVersion: tls.VersionTLS13,
			wantCiphers:    nil,
		},
		{
			name: "Old returns TLS 1.0 with nil ciphers",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileOldType,
			},
			wantMinVersion: tls.VersionTLS10,
			wantCiphers:    nil,
		},
		{
			name: "Custom with valid ciphers",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						MinTLSVersion: "VersionTLS12",
						Ciphers:       []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "ECDHE-RSA-AES256-GCM-SHA384"},
					},
				},
			},
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384},
		},
		{
			name: "Custom with unsupported cipher skips it",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						MinTLSVersion: "VersionTLS12",
						Ciphers:       []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "UNSUPPORTED-CIPHER"},
					},
				},
			},
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		},
		{
			name: "Custom with all unsupported ciphers returns empty slice",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						MinTLSVersion: "VersionTLS12",
						Ciphers:       []string{"DHE-RSA-AES128-GCM-SHA256"},
					},
				},
			},
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    []uint16{},
		},
		{
			name: "Unknown type falls back to Intermediate",
			profile: &configv1.TLSSecurityProfile{
				Type: "SuperSecure",
			},
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMinVersion, gotCiphers := parseProfile(tt.profile)

			if gotMinVersion != tt.wantMinVersion {
				t.Errorf("parseProfile() minVersion = %d, want %d", gotMinVersion, tt.wantMinVersion)
			}

			if tt.wantCiphers == nil {
				if gotCiphers != nil {
					t.Errorf("parseProfile() ciphers = %v, want nil", gotCiphers)
				}
				return
			}

			if gotCiphers == nil {
				t.Fatal("expected non-nil empty slice, got nil")
			}
			if len(gotCiphers) != len(tt.wantCiphers) {
				t.Errorf("parseProfile() ciphers length = %d, want %d", len(gotCiphers), len(tt.wantCiphers))
				return
			}
			for i, c := range gotCiphers {
				if c != tt.wantCiphers[i] {
					t.Errorf("parseProfile() ciphers[%d] = %d, want %d", i, c, tt.wantCiphers[i])
				}
			}
		})
	}
}
