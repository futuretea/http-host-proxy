package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStripPort tests the StripPort function
func TestStripPort(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "host with port",
			host:     "example.com:8080",
			expected: "example.com",
		},
		{
			name:     "host without port",
			host:     "example.com",
			expected: "example.com",
		},
		{
			name:     "localhost with port",
			host:     "localhost:3000",
			expected: "localhost",
		},
		{
			name:     "IPv4 with port",
			host:     "192.168.1.1:8080",
			expected: "192.168.1.1",
		},
		{
			name:     "IPv4 without port",
			host:     "192.168.1.1",
			expected: "192.168.1.1",
		},
		{
			name:     "empty string",
			host:     "",
			expected: "",
		},
		{
			name:     "IPv6 with port",
			host:     "[::1]:8080",
			expected: "::1",
		},
		{
			name:     "IPv6 without port (brackets)",
			host:     "[::1]",
			expected: "[::1]", // SplitHostPort returns error, so returns original
		},
		{
			name:     "IPv6 full address with port",
			host:     "[2001:db8::1]:8080",
			expected: "2001:db8::1",
		},
		{
			name:     "host with multiple colons (invalid but handled)",
			host:     "invalid:host:8080",
			expected: "invalid:host:8080", // SplitHostPort fails, returns original
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripPort(tt.host)
			if result != tt.expected {
				t.Errorf("StripPort(%q) = %q, want %q", tt.host, result, tt.expected)
			}
		})
	}
}

// TestNormalizeHost tests the NormalizeHost function
func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "lowercase with port",
			host:     "example.com:8080",
			expected: "example.com",
		},
		{
			name:     "uppercase with port",
			host:     "EXAMPLE.COM:8080",
			expected: "example.com",
		},
		{
			name:     "mixed case without port",
			host:     "ExAmPlE.CoM",
			expected: "example.com",
		},
		{
			name:     "already lowercase",
			host:     "example.com",
			expected: "example.com",
		},
		{
			name:     "empty string",
			host:     "",
			expected: "",
		},
		{
			name:     "IPv6 with port and uppercase",
			host:     "[2001:DB8::1]:8080",
			expected: "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeHost(tt.host)
			if result != tt.expected {
				t.Errorf("NormalizeHost(%q) = %q, want %q", tt.host, result, tt.expected)
			}
		})
	}
}

// TestIsHostAllowed tests the IsHostAllowed function
func TestIsHostAllowed(t *testing.T) {
	tests := []struct {
		name         string
		host         string
		allowedHosts map[string]struct{}
		blockedHosts map[string]struct{}
		expected     bool
	}{
		{
			name:         "no access control - allow all",
			host:         "example.com",
			allowedHosts: map[string]struct{}{},
			blockedHosts: map[string]struct{}{},
			expected:     true,
		},
		{
			name: "allowed host passes",
			host: "example.com",
			allowedHosts: map[string]struct{}{
				"example.com": {},
			},
			blockedHosts: map[string]struct{}{},
			expected:     true,
		},
		{
			name: "not in allowed list - denied",
			host: "other.com",
			allowedHosts: map[string]struct{}{
				"example.com": {},
			},
			blockedHosts: map[string]struct{}{},
			expected:     false,
		},
		{
			name:         "blocked host denied",
			host:         "blocked.com",
			allowedHosts: map[string]struct{}{},
			blockedHosts: map[string]struct{}{
				"blocked.com": {},
			},
			expected: false,
		},
		{
			name:         "not in blocked list - allowed",
			host:         "allowed.com",
			allowedHosts: map[string]struct{}{},
			blockedHosts: map[string]struct{}{
				"blocked.com": {},
			},
			expected: true,
		},
		{
			name: "allowed takes precedence over blocked",
			host: "example.com",
			allowedHosts: map[string]struct{}{
				"example.com": {},
			},
			blockedHosts: map[string]struct{}{
				"example.com": {},
			},
			expected: true,
		},
		{
			name: "case insensitive matching",
			host: "EXAMPLE.COM",
			allowedHosts: map[string]struct{}{
				"example.com": {},
			},
			blockedHosts: map[string]struct{}{},
			expected:     true,
		},
		{
			name: "host with port - port stripped",
			host: "example.com:8080",
			allowedHosts: map[string]struct{}{
				"example.com": {},
			},
			blockedHosts: map[string]struct{}{},
			expected:     true,
		},
		{
			name:         "blocked host with port",
			host:         "blocked.com:8080",
			allowedHosts: map[string]struct{}{},
			blockedHosts: map[string]struct{}{
				"blocked.com": {},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsHostAllowed(tt.host, tt.allowedHosts, tt.blockedHosts)
			if result != tt.expected {
				t.Errorf("IsHostAllowed(%q, %v, %v) = %v, want %v",
					tt.host, tt.allowedHosts, tt.blockedHosts, result, tt.expected)
			}
		})
	}
}

// TestDetectScheme tests the DetectScheme function
func TestDetectScheme(t *testing.T) {
	tests := []struct {
		name            string
		tls             bool
		xForwardedProto string
		expected        string
	}{
		{
			name:            "direct TLS connection",
			tls:             true,
			xForwardedProto: "",
			expected:        "https",
		},
		{
			name:            "no TLS, no header",
			tls:             false,
			xForwardedProto: "",
			expected:        "http",
		},
		{
			name:            "no TLS, X-Forwarded-Proto https",
			tls:             false,
			xForwardedProto: "https",
			expected:        "https",
		},
		{
			name:            "no TLS, X-Forwarded-Proto http",
			tls:             false,
			xForwardedProto: "http",
			expected:        "http",
		},
		{
			name:            "TLS takes precedence over header",
			tls:             true,
			xForwardedProto: "http",
			expected:        "https",
		},
		{
			name:            "X-Forwarded-Proto uppercase",
			tls:             false,
			xForwardedProto: "HTTPS",
			expected:        "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)

			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}

			if tt.xForwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.xForwardedProto)
			}

			result := DetectScheme(req)
			if result != tt.expected {
				t.Errorf("DetectScheme() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestStripHopByHopHeaders tests the StripHopByHopHeaders function
func TestStripHopByHopHeaders(t *testing.T) {
	headers := http.Header{}

	// Add hop-by-hop headers
	headers.Set("Connection", "keep-alive")
	headers.Set("Keep-Alive", "timeout=5")
	headers.Set("Proxy-Authenticate", "Basic")
	headers.Set("Proxy-Authorization", "Basic xyz")
	headers.Set("Proxy-Connection", "keep-alive")
	headers.Set("Te", "trailers")
	headers.Set("Trailer", "X-Checksum")
	headers.Set("Transfer-Encoding", "chunked")
	headers.Set("Upgrade", "websocket")

	// Add normal headers that should be preserved
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Custom-Header", "custom-value")
	headers.Set("Authorization", "Bearer token")

	StripHopByHopHeaders(headers)

	// Check hop-by-hop headers are removed
	hopByHop := []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Proxy-Connection", "Te",
		"Trailer", "Transfer-Encoding", "Upgrade",
	}

	for _, h := range hopByHop {
		if headers.Get(h) != "" {
			t.Errorf("Hop-by-hop header %q should be removed, but found %q", h, headers.Get(h))
		}
	}

	// Check normal headers are preserved
	if headers.Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be preserved")
	}
	if headers.Get("X-Custom-Header") != "custom-value" {
		t.Error("X-Custom-Header should be preserved")
	}
	if headers.Get("Authorization") != "Bearer token" {
		t.Error("Authorization should be preserved")
	}
}

// TestGetClientIP tests the GetClientIP function
func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		xForwardedFor  string
		expectedResult string
	}{
		{
			name:           "from RemoteAddr with port",
			remoteAddr:     "192.168.1.1:12345",
			xForwardedFor:  "",
			expectedResult: "192.168.1.1",
		},
		{
			name:           "from X-Forwarded-For single IP",
			remoteAddr:     "127.0.0.1:12345",
			xForwardedFor:  "203.0.113.195",
			expectedResult: "127.0.0.1", // GetClientIP always returns RemoteAddr, XFF is handled by AppendXForwardedFor
		},
		{
			name:           "from X-Forwarded-For multiple IPs",
			remoteAddr:     "127.0.0.1:12345",
			xForwardedFor:  "203.0.113.195, 70.41.3.18",
			expectedResult: "127.0.0.1", // GetClientIP always returns RemoteAddr, XFF is handled by AppendXForwardedFor
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr

			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}

			result := GetClientIP(req)
			if result != tt.expectedResult {
				t.Errorf("GetClientIP() = %q, want %q", result, tt.expectedResult)
			}
		})
	}
}

// TestAppendXForwardedFor tests the AppendXForwardedFor function
func TestAppendXForwardedFor(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		clientIP string
		expected string
	}{
		{
			name:     "no existing header",
			existing: "",
			clientIP: "192.168.1.1",
			expected: "192.168.1.1",
		},
		{
			name:     "append to existing",
			existing: "10.0.0.1",
			clientIP: "192.168.1.1",
			expected: "10.0.0.1, 192.168.1.1",
		},
		{
			name:     "append to multiple existing",
			existing: "10.0.0.1, 172.16.0.1",
			clientIP: "192.168.1.1",
			expected: "10.0.0.1, 172.16.0.1, 192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			if tt.existing != "" {
				header.Set("X-Forwarded-For", tt.existing)
			}

			AppendXForwardedFor(header, tt.clientIP)

			result := header.Get("X-Forwarded-For")
			if result != tt.expected {
				t.Errorf("AppendXForwardedFor() result = %q, want %q", result, tt.expected)
			}
		})
	}
}
