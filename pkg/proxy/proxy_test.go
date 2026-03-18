package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/futuretea/http-host-proxy/pkg/config"
)

// createTestConfig creates a test configuration with defaults.
func createTestConfig() *config.Config {
	return &config.Config{
		ProxyListen:           ":8080",
		DefaultScheme:         "http",
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90,
		DialTimeout:           30,
		TLSHandshakeTimeout:   10,
		ResponseHeaderTimeout: 30,
		TLSInsecure:           true,
	}
}

// TestProxyNew tests the proxy constructor
func TestProxyNew(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		expectError bool
	}{
		{
			name:        "valid config",
			cfg:         createTestConfig(),
			expectError: false,
		},
		{
			name: "config with allowed hosts",
			cfg: func() *config.Config {
				cfg := createTestConfig()
				cfg.AllowedHosts = []string{"example.com", "test.com"}
				return cfg
			}(),
			expectError: false,
		},
		{
			name: "config with blocked hosts",
			cfg: func() *config.Config {
				cfg := createTestConfig()
				cfg.BlockedHosts = []string{"blocked.com"}
				return cfg
			}(),
			expectError: false,
		},
		{
			name: "config with host scheme map",
			cfg: func() *config.Config {
				cfg := createTestConfig()
				cfg.HostSchemeMap = map[string]string{
					"secure.com": "https",
				}
				return cfg
			}(),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, err := New(tt.cfg)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if proxy == nil {
				t.Error("expected proxy but got nil")
				return
			}

			// Verify proxy was configured correctly
			if proxy.config != tt.cfg {
				t.Error("config not set correctly")
			}
		})
	}
}

// TestProxyBasicForwarding tests basic request forwarding
func TestProxyBasicForwarding(t *testing.T) {
	// Create a mock backend server that echoes request details
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Path", r.URL.Path)
		w.Header().Set("X-Backend-Query", r.URL.RawQuery)
		w.Header().Set("X-Backend-Method", r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Get backend host (without scheme)
	backendHost := strings.TrimPrefix(backend.URL, "http://")

	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Create test request with Host header pointing to backend
	req := httptest.NewRequest("GET", "/api/test?foo=bar", nil)
	req.Host = backendHost

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	// Verify the backend received the request
	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// Verify path was preserved
	if path := rr.Header().Get("X-Backend-Path"); path != "/api/test" {
		t.Errorf("backend path = %q, want %q", path, "/api/test")
	}

	// Verify query was preserved
	if query := rr.Header().Get("X-Backend-Query"); query != "foo=bar" {
		t.Errorf("backend query = %q, want %q", query, "foo=bar")
	}

	// Verify method was preserved
	if method := rr.Header().Get("X-Backend-Method"); method != "GET" {
		t.Errorf("backend method = %q, want %q", method, "GET")
	}
}

// TestProxyBlockedHost tests that blocked hosts return 403
func TestProxyBlockedHost(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not be called for blocked host")
	}))
	defer backend.Close()

	cfg := createTestConfig()
	cfg.BlockedHosts = []string{"blocked.example.com"}

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = "blocked.example.com"

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusForbidden)
	}

	// Verify JSON error response
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if resp["error"] != "Forbidden" {
		t.Errorf("error = %q, want %q", resp["error"], "Forbidden")
	}
}

// TestProxyAllowedHostEnforcement tests that unlisted hosts are denied when allowed_hosts is set
func TestProxyAllowedHostEnforcement(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not be called for unlisted host")
	}))
	defer backend.Close()

	cfg := createTestConfig()
	cfg.AllowedHosts = []string{"allowed.example.com"}

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = "notallowed.example.com"

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

// TestProxyAllowedHostPass tests that allowed hosts pass through
func TestProxyAllowedHostPass(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	backendHost := strings.TrimPrefix(backend.URL, "http://")

	cfg := createTestConfig()
	cfg.AllowedHosts = []string{backendHost}

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = backendHost

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
}

// TestProxyXForwardedHeaders tests X-Forwarded-* headers are set correctly
func TestProxyXForwardedHeaders(t *testing.T) {
	var capturedHeaders http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendHost := strings.TrimPrefix(backend.URL, "http://")

	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = backendHost
	req.RemoteAddr = "192.168.1.100:12345"

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// Check X-Forwarded-For
	xff := capturedHeaders.Get("X-Forwarded-For")
	if !strings.Contains(xff, "192.168.1.100") {
		t.Errorf("X-Forwarded-For = %q, should contain client IP", xff)
	}

	// Check X-Forwarded-Host
	xfh := capturedHeaders.Get("X-Forwarded-Host")
	if xfh != backendHost {
		t.Errorf("X-Forwarded-Host = %q, want %q", xfh, backendHost)
	}

	// Check X-Forwarded-Proto
	xfp := capturedHeaders.Get("X-Forwarded-Proto")
	if xfp != "http" {
		t.Errorf("X-Forwarded-Proto = %q, want %q", xfp, "http")
	}
}

// TestProxyXRequestID tests X-Request-ID generation and propagation
func TestProxyXRequestID(t *testing.T) {
	var capturedRequestID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendHost := strings.TrimPrefix(backend.URL, "http://")

	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Request without X-Request-ID
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = backendHost

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// Verify X-Request-ID was generated and propagated
	if capturedRequestID == "" {
		t.Error("X-Request-ID should be generated and propagated to backend")
	}

	// Verify it's a valid hex string (8 chars for 4 bytes)
	if len(capturedRequestID) != 8 {
		t.Errorf("X-Request-ID length = %d, want 8", len(capturedRequestID))
	}
}

// TestProxyExistingXRequestID tests that existing X-Request-ID is preserved
func TestProxyExistingXRequestID(t *testing.T) {
	var capturedRequestID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendHost := strings.TrimPrefix(backend.URL, "http://")

	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Request with existing X-Request-ID
	existingID := "existing-request-id-123"
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = backendHost
	req.Header.Set("X-Request-ID", existingID)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// Verify existing X-Request-ID was preserved
	if capturedRequestID != existingID {
		t.Errorf("X-Request-ID = %q, want %q", capturedRequestID, existingID)
	}
}

// TestProxyBackendDown tests error handling when backend is unreachable
func TestProxyBackendDown(t *testing.T) {
	cfg := createTestConfig()
	cfg.DialTimeout = 1 // Short timeout for faster test

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Use an unreachable address
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = "127.0.0.1:59999" // Unlikely to have a service running

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	// Should return 502 Bad Gateway
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadGateway)
	}

	// Verify JSON error response
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if resp["error"] != "Bad Gateway" {
		t.Errorf("error = %q, want %q", resp["error"], "Bad Gateway")
	}
}

// TestProxyHopByHopStripping tests that hop-by-hop headers are not forwarded
func TestProxyHopByHopStripping(t *testing.T) {
	var capturedHeaders http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendHost := strings.TrimPrefix(backend.URL, "http://")

	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = backendHost
	// Add hop-by-hop headers
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Proxy-Authorization", "Basic xyz")
	// Note: We don't test "Te: trailers" because Go's stdlib intentionally
	// preserves it per RFC 7230 to signal trailer support to backends

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// Verify hop-by-hop headers were stripped
	// Note: "Te" with value "trailers" is preserved by Go's stdlib per RFC 7230
	hopByHop := []string{"Connection", "Keep-Alive", "Proxy-Authorization"}
	for _, h := range hopByHop {
		if capturedHeaders.Get(h) != "" {
			t.Errorf("hop-by-hop header %q should be stripped", h)
		}
	}
}

// TestProxyExtraHeaders tests that extra request headers are injected
func TestProxyExtraHeaders(t *testing.T) {
	var capturedHeaders http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendHost := strings.TrimPrefix(backend.URL, "http://")

	cfg := createTestConfig()
	cfg.ExtraRequestHeaders = map[string]string{
		"X-Custom-Header": "custom-value",
		"X-Another":       "another-value",
	}

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = backendHost

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// Verify extra headers were injected
	if capturedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want %q", capturedHeaders.Get("X-Custom-Header"), "custom-value")
	}
	if capturedHeaders.Get("X-Another") != "another-value" {
		t.Errorf("X-Another = %q, want %q", capturedHeaders.Get("X-Another"), "another-value")
	}
}

// TestProxyHealthHandler tests the health check handler
func TestProxyHealthHandler(t *testing.T) {
	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	p.HealthHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var resp map[string]interface{}
	body, _ := io.ReadAll(rr.Body)
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("status = %q, want %q", resp["status"], "healthy")
	}
	if resp["service"] != "http-host-proxy" {
		t.Errorf("service = %q, want %q", resp["service"], "http-host-proxy")
	}
}

// TestProxyReadinessHandler tests the readiness check handler
func TestProxyReadinessHandler(t *testing.T) {
	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Test ready state
	req := httptest.NewRequest("GET", "/readyz", nil)
	rr := httptest.NewRecorder()
	p.ReadinessHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	body, _ := io.ReadAll(rr.Body)
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if resp["status"] != "ready" {
		t.Errorf("status = %q, want %q", resp["status"], "ready")
	}
	if resp["ready"] != true {
		t.Errorf("ready = %v, want %v", resp["ready"], true)
	}

	// Test shutting down state
	p.SetShuttingDown()

	req = httptest.NewRequest("GET", "/readyz", nil)
	rr = httptest.NewRecorder()
	p.ReadinessHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}

	body, _ = io.ReadAll(rr.Body)
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if resp["status"] != "shutting_down" {
		t.Errorf("status = %q, want %q", resp["status"], "shutting_down")
	}
	if resp["ready"] != false {
		t.Errorf("ready = %v, want %v", resp["ready"], false)
	}
}

// TestProxyUpdateConfig tests hot config reload
func TestProxyUpdateConfig(t *testing.T) {
	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Initially no blocked hosts
	allowed, blocked := p.GetAccessControlMaps()
	if len(blocked) != 0 {
		t.Errorf("initial blocked hosts = %d, want 0", len(blocked))
	}
	if len(allowed) != 0 {
		t.Errorf("initial allowed hosts = %d, want 0", len(allowed))
	}

	// Update config with blocked hosts
	newCfg := createTestConfig()
	newCfg.BlockedHosts = []string{"blocked.com"}
	newCfg.AllowedHosts = []string{"allowed.com"}

	p.UpdateConfig(newCfg)

	// Verify config was updated
	allowed, blocked = p.GetAccessControlMaps()
	if len(blocked) != 1 {
		t.Errorf("blocked hosts = %d, want 1", len(blocked))
	}
	if len(allowed) != 1 {
		t.Errorf("allowed hosts = %d, want 1", len(allowed))
	}
}

// TestProxySetShuttingDown tests the shutdown flag
func TestProxySetShuttingDown(t *testing.T) {
	cfg := createTestConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Initially not shutting down
	if p.IsShuttingDown() {
		t.Error("proxy should not be shutting down initially")
	}

	// Set shutting down
	p.SetShuttingDown()

	if !p.IsShuttingDown() {
		t.Error("proxy should be shutting down after SetShuttingDown()")
	}
}
