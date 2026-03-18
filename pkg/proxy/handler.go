package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/futuretea/http-host-proxy/pkg/metrics"
)

// ServeHTTP handles incoming HTTP requests.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Defensive: check for nil request
	if r == nil || r.URL == nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Track active connections
	metrics.ActiveConnections.Inc()
	defer metrics.ActiveConnections.Dec()

	// Generate X-Request-ID if not present
	reqID := r.Header.Get(headerXRequestID)
	if reqID == "" {
		reqID = generateRequestID()
	}
	r.Header.Set(headerXRequestID, reqID)

	// Detect protocol and set X-Forwarded-Proto
	proto := DetectScheme(r)
	r.Header.Set(headerXForwardedProto, proto)

	// Get host for access control check
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}

	// Access control check - done here before proxying
	allowedHosts, blockedHosts := p.GetAccessControlMaps()
	if !IsHostAllowed(host, allowedHosts, blockedHosts) {
		// Record blocked request metric
		metrics.RecordBlockedRequest()

		// Log blocked request at info level
		log.Info().
			Str("request_id", reqID).
			Str("host", host).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Msg("Request blocked by access control")

		// Write 403 Forbidden response
		w.Header().Set(headerContentType, "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  "Forbidden",
			"status": http.StatusForbidden,
		})
		return
	}

	// Wrap ResponseWriter to capture metrics
	mw := metrics.NewResponseWriter(w)

	// Limit request body size if configured
	if p.config.MaxRequestBodySize > 0 && r.Body != nil {
		r.Body = http.MaxBytesReader(mw, r.Body, p.config.MaxRequestBodySize)
	}

	// Record start time
	startTime := time.Now()

	// Serve the request through the reverse proxy
	p.reverseProxy.ServeHTTP(mw, r)

	// Calculate duration
	duration := time.Since(startTime)

	// Get target host (may have been modified by director)
	targetHost := r.URL.Host
	if targetHost == "" {
		targetHost = host
	}

	// Record request metrics
	metrics.RecordRequest(
		r.Method,
		r.URL.Path,
		targetHost,
		mw.StatusCode(),
		duration,
		r.ContentLength,   // bytes received from client
		mw.BytesWritten(), // bytes sent to client
	)

	// Structured access log at debug level
	log.Debug().
		Str("request_id", reqID).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("host", host).
		Str("remote_addr", r.RemoteAddr).
		Int("status", mw.StatusCode()).
		Dur("duration_ms", duration).
		Int64("bytes_in", r.ContentLength).
		Int64("bytes_out", mw.BytesWritten()).
		Msg("Request completed")
}

// generateRequestID creates a short random ID for request correlation.
// Returns a random hex string, or a fallback timestamp-based ID on error.
func generateRequestID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		// This should almost never happen with crypto/rand
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano()))[:4])
	}
	return hex.EncodeToString(b)
}

// HealthHandler handles health check requests.
// Returns 200 OK with JSON response indicating healthy status.
func (p *Proxy) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(headerContentType, "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"service": "http-host-proxy",
	})
}

// ReadinessHandler handles readiness check requests.
// Returns 200 OK when ready to serve traffic, 503 when shutting down.
func (p *Proxy) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(headerContentType, "application/json")

	if p.IsShuttingDown() {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "shutting_down",
			"ready":  false,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ready",
		"ready":  true,
	})
}
