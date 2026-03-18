package proxy

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/futuretea/http-host-proxy/pkg/config"
	"github.com/futuretea/http-host-proxy/pkg/metrics"
)

const (
	// Header names
	headerXForwardedFor   = "X-Forwarded-For"
	headerXForwardedHost  = "X-Forwarded-Host"
	headerXForwardedProto = "X-Forwarded-Proto"
	headerXRequestID      = "X-Request-ID"
	headerContentType     = "Content-Type"

	// Protocol schemes
	schemeHTTP  = "http"
	schemeHTTPS = "https"

	// Buffer sizes
	bufferSize = 32 * 1024 // 32KB for BufferPool
)

// bufferPool implements httputil.BufferPool using sync.Pool with 32KB buffers.
type bufferPool struct {
	pool sync.Pool
}

// Get returns a buffer from the pool.
func (bp *bufferPool) Get() []byte {
	if buf := bp.pool.Get(); buf != nil {
		return buf.([]byte)
	}
	return make([]byte, bufferSize)
}

// Put returns a buffer to the pool.
func (bp *bufferPool) Put(buf []byte) {
	bp.pool.Put(buf)
}

// Proxy wraps the reverse proxy with host-based routing configuration.
type Proxy struct {
	reverseProxy *httputil.ReverseProxy
	config       *config.Config
	shuttingDown int32 // atomic flag for graceful shutdown

	// Hot-reloadable fields protected by RWMutex
	mu            sync.RWMutex
	allowedHosts  map[string]struct{}
	blockedHosts  map[string]struct{}
	hostSchemeMap map[string]string
	extraHeaders  map[string]string
}

// New creates a new HTTP host proxy instance.
func New(cfg *config.Config) (*Proxy, error) {
	// Initialize proxy struct with config
	// Normalize allowed/blocked hosts to strip ports for consistent matching
	p := &Proxy{
		config:        cfg,
		allowedHosts:  normalizeHostMap(cfg.GetAllowedHostsMap()),
		blockedHosts:  normalizeHostMap(cfg.GetBlockedHostsMap()),
		hostSchemeMap: cfg.GetHostSchemeMap(),
		extraHeaders:  cfg.ExtraRequestHeaders,
	}

	// Create HTTP transport with configurable settings
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(cfg.DialTimeout) * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecure,
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       time.Duration(cfg.IdleConnTimeout) * time.Second,
		TLSHandshakeTimeout:   time.Duration(cfg.TLSHandshakeTimeout) * time.Second,
		ResponseHeaderTimeout: time.Duration(cfg.ResponseHeaderTimeout) * time.Second,
		DisableCompression:    true,
		WriteBufferSize:       64 * 1024, // 64KB write buffer
		ReadBufferSize:        64 * 1024, // 64KB read buffer
	}

	// Create reverse proxy
	reverseProxy := &httputil.ReverseProxy{
		Director:      p.director,
		ErrorHandler:  p.errorHandler,
		BufferPool:    &bufferPool{},
		FlushInterval: -1, // Immediate streaming
		Transport:     transport,
	}

	p.reverseProxy = reverseProxy
	return p, nil
}

// director is the custom Director function for routing requests.
func (p *Proxy) director(req *http.Request) {
	// Extract host from request
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	// Normalize host for lookup (lowercase, strip port)
	normalizedHost := NormalizeHost(host)

	// Read-lock for hot-reloadable config
	p.mu.RLock()
	hostSchemeMap := p.hostSchemeMap
	extraHeaders := p.extraHeaders
	defaultScheme := p.config.DefaultScheme
	p.mu.RUnlock()

	// Resolve scheme: check hostSchemeMap first, fall back to default
	scheme := defaultScheme
	if mappedScheme, ok := hostSchemeMap[normalizedHost]; ok {
		scheme = mappedScheme
	}

	// Set URL scheme and host for forwarding
	req.URL.Scheme = scheme
	req.URL.Host = host // Use original host with port

	// Get client IP for X-Forwarded-For
	clientIP := GetClientIP(req)

	// Set standard proxy headers
	AppendXForwardedFor(req.Header, clientIP)
	req.Header.Set(headerXForwardedHost, host)

	// X-Forwarded-Proto should already be set in ServeHTTP
	// but set default if not present
	if req.Header.Get(headerXForwardedProto) == "" {
		req.Header.Set(headerXForwardedProto, schemeHTTP)
	}

	// Strip hop-by-hop headers
	StripHopByHopHeaders(req.Header)

	// Inject extra request headers from config
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}
}

// errorHandler handles errors from the reverse proxy.
func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	// Classify the error
	var errorType string
	var statusCode int
	var statusText string

	errStr := err.Error()

	switch {
	case isTimeoutError(err):
		errorType = "connect_timeout"
		statusCode = http.StatusGatewayTimeout
		statusText = "Gateway Timeout"
	case strings.Contains(errStr, "connection refused"):
		errorType = "connect_refused"
		statusCode = http.StatusBadGateway
		statusText = "Bad Gateway"
	case strings.Contains(errStr, "tls:") || strings.Contains(errStr, "x509:"):
		errorType = "tls_error"
		statusCode = http.StatusBadGateway
		statusText = "Bad Gateway"
	case strings.Contains(errStr, "no such host") || strings.Contains(errStr, "lookup"):
		errorType = "dns_error"
		statusCode = http.StatusBadGateway
		statusText = "Bad Gateway"
	default:
		errorType = "other"
		statusCode = http.StatusBadGateway
		statusText = "Bad Gateway"
	}

	// Get target host from request
	targetHost := r.URL.Host
	if targetHost == "" {
		targetHost = r.Host
	}

	// Record metrics
	metrics.RecordHostError(targetHost, errorType)

	// Get request ID for logging
	reqID := r.Header.Get(headerXRequestID)

	// Log the error
	log.Warn().
		Str("request_id", reqID).
		Str("target_host", targetHost).
		Str("error_type", errorType).
		Err(err).
		Msg("Proxy error")

	// Write JSON error response
	w.Header().Set(headerContentType, "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  statusText,
		"status": statusCode,
	})
}

// isTimeoutError checks if the error is a timeout error.
func isTimeoutError(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// UpdateConfig updates the hot-reloadable configuration fields.
func (p *Proxy) UpdateConfig(cfg *config.Config) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.config = cfg
	p.allowedHosts = normalizeHostMap(cfg.GetAllowedHostsMap())
	p.blockedHosts = normalizeHostMap(cfg.GetBlockedHostsMap())
	p.hostSchemeMap = cfg.GetHostSchemeMap()
	p.extraHeaders = cfg.ExtraRequestHeaders
}

// normalizeHostMap normalizes all keys in a host map by stripping ports.
// This ensures consistent matching regardless of whether hosts include ports.
func normalizeHostMap(m map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(m))
	for host := range m {
		result[NormalizeHost(host)] = struct{}{}
	}
	return result
}

// SetShuttingDown marks the proxy as shutting down.
// This will cause readiness checks to fail.
func (p *Proxy) SetShuttingDown() {
	atomic.StoreInt32(&p.shuttingDown, 1)
}

// IsShuttingDown returns true if the proxy is shutting down.
func (p *Proxy) IsShuttingDown() bool {
	return atomic.LoadInt32(&p.shuttingDown) == 1
}

// GetAccessControlMaps returns the current allowed and blocked hosts maps.
// This is used by ServeHTTP to check access control.
func (p *Proxy) GetAccessControlMaps() (map[string]struct{}, map[string]struct{}) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.allowedHosts, p.blockedHosts
}
