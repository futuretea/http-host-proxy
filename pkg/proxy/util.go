package proxy

import (
	"net"
	"net/http"
	"strings"
)

// hopByHopHeaders lists headers that should not be forwarded.
// These are connection-specific and should be removed by proxies.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// StripPort removes the port from a host string using net.SplitHostPort.
// Handles IPv4, IPv6, and hostname formats correctly.
// "example.com:8080" → "example.com"
// "[::1]:8080" → "::1"
// "example.com" → "example.com"
func StripPort(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		// If SplitHostPort fails, it means there's no port or invalid format
		// Return original string (likely just a hostname)
		return hostport
	}
	return host
}

// NormalizeHost lowercases and strips port from a host string.
// This is used for consistent host matching in access control.
func NormalizeHost(host string) string {
	return strings.ToLower(StripPort(host))
}

// IsHostAllowed checks if a host is permitted given allowed and blocked maps.
// If allowedHosts is non-empty, host must be in it.
// If blockedHosts is non-empty, host must not be in it.
// If both are set, allowedHosts takes precedence (whitelist mode).
// Returns true if the host is allowed, false if blocked.
func IsHostAllowed(host string, allowedHosts, blockedHosts map[string]struct{}) bool {
	normalizedHost := NormalizeHost(host)

	// If allowed hosts is configured (whitelist mode), host must be in list
	if len(allowedHosts) > 0 {
		_, allowed := allowedHosts[normalizedHost]
		return allowed
	}

	// If blocked hosts is configured (blacklist mode), host must not be in list
	if len(blockedHosts) > 0 {
		_, blocked := blockedHosts[normalizedHost]
		return !blocked
	}

	// No access control configured, allow all
	return true
}

// DetectScheme returns "https" if the request is over TLS, otherwise "http".
// It checks both the TLS field and X-Forwarded-Proto header.
func DetectScheme(r *http.Request) string {
	// Check if request came over TLS directly
	if r.TLS != nil {
		return "https"
	}

	// Check X-Forwarded-Proto header (set by load balancers/proxies)
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.ToLower(proto)
	}

	// Default to http
	return "http"
}

// StripHopByHopHeaders removes hop-by-hop headers from a request.
// These headers are connection-specific and should not be forwarded.
func StripHopByHopHeaders(header http.Header) {
	for _, h := range hopByHopHeaders {
		header.Del(h)
	}
}

// GetClientIP extracts the client IP from the request's RemoteAddr.
// It always returns only the immediate client's IP address.
// The existing X-Forwarded-For chain is preserved by AppendXForwardedFor().
func GetClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// AppendXForwardedFor appends the client IP to the X-Forwarded-For header.
func AppendXForwardedFor(header http.Header, clientIP string) {
	if existing := header.Get("X-Forwarded-For"); existing != "" {
		header.Set("X-Forwarded-For", existing+", "+clientIP)
	} else {
		header.Set("X-Forwarded-For", clientIP)
	}
}
