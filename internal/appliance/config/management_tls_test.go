package config

import "testing"

func TestManagementTLSConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name    string
		env     map[string]string
		enabled bool
		invalid bool
	}{
		{
			name: "default",
		},
		{
			name: "enabled",
			env: map[string]string{
				"RACKOS_MANAGEMENT_TLS": "true",
			},
			enabled: true,
		},
		{
			name: "disabled",
			env: map[string]string{
				"RACKOS_MANAGEMENT_TLS": "false",
			},
		},
		{
			name: "invalid",
			env: map[string]string{
				"RACKOS_MANAGEMENT_TLS": "invalid",
			},
			invalid: true,
		},
		{
			name: "empty",
			env: map[string]string{
				"RACKOS_MANAGEMENT_TLS": "",
			},
			invalid: true,
		},
		{
			name: "ambiguous",
			env: map[string]string{
				"RACKOS_MANAGEMENT_TLS": "1",
			},
			invalid: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadTestEnvironment(tc.env)

			if tc.invalid {
				if err == nil {
					t.Fatal("invalid TLS setting accepted")
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if cfg.ManagementTLS != tc.enabled {
				t.Fatal("incorrect management TLS setting")
			}
		})
	}
}
