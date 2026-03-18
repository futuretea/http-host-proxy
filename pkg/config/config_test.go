package config

import (
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with defaults applied",
			config: Config{
				ProxyListen:   ":8080",
				DefaultScheme: "http",
				LogLevel:      "info",
			},
			wantErr: false,
		},
		{
			name: "valid config with https scheme",
			config: Config{
				ProxyListen:   ":8080",
				DefaultScheme: "https",
				LogLevel:      "info",
			},
			wantErr: false,
		},
		{
			name: "invalid default scheme",
			config: Config{
				ProxyListen:   ":8080",
				DefaultScheme: "ftp",
				LogLevel:      "info",
			},
			wantErr: true,
			errMsg:  "default_scheme must be 'http' or 'https'",
		},
		{
			name: "invalid host scheme map value",
			config: Config{
				ProxyListen:   ":8080",
				DefaultScheme: "http",
				HostSchemeMap: map[string]string{
					"example.com": "ftp",
				},
				LogLevel: "info",
			},
			wantErr: true,
			errMsg:  "host_scheme_map value for 'example.com' must be 'http' or 'https'",
		},
		{
			name: "missing proxy listen",
			config: Config{
				ProxyListen:   "",
				DefaultScheme: "http",
				LogLevel:      "info",
			},
			wantErr: true,
			errMsg:  "proxy_listen is required",
		},
		{
			name: "invalid log level",
			config: Config{
				ProxyListen:   ":8080",
				DefaultScheme: "http",
				LogLevel:      "invalid",
			},
			wantErr: true,
			errMsg:  "log_level must be one of",
		},
		{
			name: "TLS enabled without cert file",
			config: Config{
				ProxyListen:   ":8080",
				DefaultScheme: "http",
				LogLevel:      "info",
				TLSEnabled:    true,
				TLSKeyFile:    "/path/to/key.pem",
			},
			wantErr: true,
			errMsg:  "tls_cert_file is required when tls_enabled is true",
		},
		{
			name: "TLS enabled without key file",
			config: Config{
				ProxyListen:   ":8080",
				DefaultScheme: "http",
				LogLevel:      "info",
				TLSEnabled:    true,
				TLSCertFile:   "/path/to/cert.pem",
			},
			wantErr: true,
			errMsg:  "tls_key_file is required when tls_enabled is true",
		},
		{
			name: "valid TLS config",
			config: Config{
				ProxyListen:   ":8080",
				DefaultScheme: "http",
				LogLevel:      "info",
				TLSEnabled:    true,
				TLSCertFile:   "/path/to/cert.pem",
				TLSKeyFile:    "/path/to/key.pem",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	config := &Config{}
	applyDefaults(config)

	if config.ProxyListen != ":8080" {
		t.Errorf("expected ProxyListen to be ':8080', got %q", config.ProxyListen)
	}
	if config.DefaultScheme != "http" {
		t.Errorf("expected DefaultScheme to be 'http', got %q", config.DefaultScheme)
	}
	if config.LogLevel != "info" {
		t.Errorf("expected LogLevel to be 'info', got %q", config.LogLevel)
	}
	if config.MaxIdleConns != 200 {
		t.Errorf("expected MaxIdleConns to be 200, got %d", config.MaxIdleConns)
	}
	if config.MaxIdleConnsPerHost != 100 {
		t.Errorf("expected MaxIdleConnsPerHost to be 100, got %d", config.MaxIdleConnsPerHost)
	}
	if config.IdleConnTimeout != 90 {
		t.Errorf("expected IdleConnTimeout to be 90, got %d", config.IdleConnTimeout)
	}
	if config.DialTimeout != 30 {
		t.Errorf("expected DialTimeout to be 30, got %d", config.DialTimeout)
	}
	if config.TLSHandshakeTimeout != 10 {
		t.Errorf("expected TLSHandshakeTimeout to be 10, got %d", config.TLSHandshakeTimeout)
	}
	if config.ResponseHeaderTimeout != 30 {
		t.Errorf("expected ResponseHeaderTimeout to be 30, got %d", config.ResponseHeaderTimeout)
	}
	if config.MaxRequestBodySize != 10485760 {
		t.Errorf("expected MaxRequestBodySize to be 10485760, got %d", config.MaxRequestBodySize)
	}
}

func TestGetAllowedHostsMap(t *testing.T) {
	tests := []struct {
		name     string
		hosts    []string
		expected map[string]struct{}
	}{
		{
			name:     "empty hosts",
			hosts:    []string{},
			expected: map[string]struct{}{},
		},
		{
			name:  "single host",
			hosts: []string{"example.com"},
			expected: map[string]struct{}{
				"example.com": {},
			},
		},
		{
			name:  "multiple hosts",
			hosts: []string{"example.com", "api.example.com"},
			expected: map[string]struct{}{
				"example.com":     {},
				"api.example.com": {},
			},
		},
		{
			name:  "case insensitive normalization",
			hosts: []string{"EXAMPLE.COM", "Api.Example.Com"},
			expected: map[string]struct{}{
				"example.com":     {},
				"api.example.com": {},
			},
		},
		{
			name:  "hosts with whitespace",
			hosts: []string{" example.com ", "  api.example.com  "},
			expected: map[string]struct{}{
				"example.com":     {},
				"api.example.com": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{AllowedHosts: tt.hosts}
			result := cfg.GetAllowedHostsMap()

			if len(result) != len(tt.expected) {
				t.Errorf("got %d entries, want %d", len(result), len(tt.expected))
			}

			for k := range tt.expected {
				if _, ok := result[k]; !ok {
					t.Errorf("expected key %q not found in result", k)
				}
			}
		})
	}
}

func TestGetBlockedHostsMap(t *testing.T) {
	tests := []struct {
		name     string
		hosts    []string
		expected map[string]struct{}
	}{
		{
			name:     "empty hosts",
			hosts:    []string{},
			expected: map[string]struct{}{},
		},
		{
			name:  "single host",
			hosts: []string{"blocked.com"},
			expected: map[string]struct{}{
				"blocked.com": {},
			},
		},
		{
			name:  "multiple hosts",
			hosts: []string{"blocked.com", "spam.com"},
			expected: map[string]struct{}{
				"blocked.com": {},
				"spam.com":    {},
			},
		},
		{
			name:  "case insensitive normalization",
			hosts: []string{"BLOCKED.COM", "Spam.Com"},
			expected: map[string]struct{}{
				"blocked.com": {},
				"spam.com":    {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{BlockedHosts: tt.hosts}
			result := cfg.GetBlockedHostsMap()

			if len(result) != len(tt.expected) {
				t.Errorf("got %d entries, want %d", len(result), len(tt.expected))
			}

			for k := range tt.expected {
				if _, ok := result[k]; !ok {
					t.Errorf("expected key %q not found in result", k)
				}
			}
		})
	}
}

func TestGetHostSchemeMap(t *testing.T) {
	tests := []struct {
		name     string
		inputMap map[string]string
		expected map[string]string
	}{
		{
			name: "lowercase keys unchanged",
			inputMap: map[string]string{
				"example.com": "https",
				"api.com":     "http",
			},
			expected: map[string]string{
				"example.com": "https",
				"api.com":     "http",
			},
		},
		{
			name: "uppercase keys normalized",
			inputMap: map[string]string{
				"EXAMPLE.COM": "https",
				"API.COM":     "http",
			},
			expected: map[string]string{
				"example.com": "https",
				"api.com":     "http",
			},
		},
		{
			name: "mixed case keys normalized",
			inputMap: map[string]string{
				"Example.Com":     "https",
				"Api.Example.Com": "http",
			},
			expected: map[string]string{
				"example.com":     "https",
				"api.example.com": "http",
			},
		},
		{
			name:     "empty map",
			inputMap: map[string]string{},
			expected: map[string]string{},
		},
		{
			name:     "nil map",
			inputMap: nil,
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{HostSchemeMap: tt.inputMap}
			result := cfg.GetHostSchemeMap()

			if len(result) != len(tt.expected) {
				t.Errorf("got %d entries, want %d", len(result), len(tt.expected))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("for key %q: got %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

func TestParseKeyValueString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:  "valid single pair",
			input: "example.com=https",
			expected: map[string]string{
				"example.com": "https",
			},
		},
		{
			name:  "valid multiple pairs",
			input: "example.com=https,api.com=http",
			expected: map[string]string{
				"example.com": "https",
				"api.com":     "http",
			},
		},
		{
			name:  "with spaces",
			input: "example.com = https , api.com = http",
			expected: map[string]string{
				"example.com": "https",
				"api.com":     "http",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:  "trailing comma",
			input: "example.com=https,",
			expected: map[string]string{
				"example.com": "https",
			},
		},
		{
			name:     "invalid format (no equals)",
			input:    "example.com",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseKeyValueString(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d entries, want %d", len(result), len(tt.expected))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("for key %q: got %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

func TestParseCommaSeparatedList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single value",
			input:    "example.com",
			expected: []string{"example.com"},
		},
		{
			name:     "multiple values",
			input:    "example.com,api.com,test.com",
			expected: []string{"example.com", "api.com", "test.com"},
		},
		{
			name:     "with spaces",
			input:    " example.com , api.com , test.com ",
			expected: []string{"example.com", "api.com", "test.com"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "trailing comma",
			input:    "example.com,",
			expected: []string{"example.com"},
		},
		{
			name:     "empty values filtered",
			input:    "example.com,,api.com",
			expected: []string{"example.com", "api.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommaSeparatedList(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d entries, want %d", len(result), len(tt.expected))
			}

			for i, v := range tt.expected {
				if i < len(result) && result[i] != v {
					t.Errorf("at index %d: got %q, want %q", i, result[i], v)
				}
			}
		})
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
