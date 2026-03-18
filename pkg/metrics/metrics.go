package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "http_host_proxy"
)

var (
	// RequestsTotal counts total HTTP requests proxied
	// Labels: method, status_code, target_host
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of HTTP requests proxied.",
		},
		[]string{"method", "status_code", "target_host"},
	)

	// RequestDuration tracks request duration distribution
	// Labels: method, target_host
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 5.0, 15.0, 30.0, 60.0},
		},
		[]string{"method", "target_host"},
	)

	// BytesTransferred counts total bytes sent/received
	// Labels: direction (sent/received), target_host
	BytesTransferred = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "bytes_transferred_total",
			Help:      "Total bytes transferred.",
		},
		[]string{"direction", "target_host"},
	)

	// ActiveConnections tracks real-time concurrent connections
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_connections",
			Help:      "Number of active connections being proxied.",
		},
	)

	// HostErrors counts errors per target host and error type
	// Labels: target_host, error_type
	HostErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "host_errors_total",
			Help:      "Total proxy errors by target host and error type.",
		},
		[]string{"target_host", "error_type"},
	)

	// BlockedRequests counts requests denied by access control
	BlockedRequests = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "blocked_requests_total",
			Help:      "Total requests blocked by host access control.",
		},
	)
)

// PathType classifies the request path for metric labeling.
// Returns: "health", "readiness", "metrics", "proxy"
func PathType(path string) string {
	if path == "/healthz" {
		return "health"
	}
	if path == "/readyz" {
		return "readiness"
	}
	if path == "/metrics" {
		return "metrics"
	}
	return "proxy"
}

// RecordRequest records metrics for a completed request.
// Skips detailed recording for probe endpoints (health, readiness).
// Parameters: method, path, targetHost, statusCode, duration, bytesIn, bytesOut
func RecordRequest(method, path, targetHost string, statusCode int, duration time.Duration, bytesIn, bytesOut int64) {
	pathType := PathType(path)

	// Skip metrics for health checks to reduce overhead
	if pathType == "health" || pathType == "readiness" {
		return
	}

	statusStr := strconv.Itoa(statusCode)

	RequestsTotal.WithLabelValues(method, statusStr, targetHost).Inc()
	RequestDuration.WithLabelValues(method, targetHost).Observe(duration.Seconds())

	if bytesIn > 0 {
		BytesTransferred.WithLabelValues("received", targetHost).Add(float64(bytesIn))
	}
	if bytesOut > 0 {
		BytesTransferred.WithLabelValues("sent", targetHost).Add(float64(bytesOut))
	}
}

// RecordHostError records an upstream error with classification.
// errorType should be one of: "connect_refused", "connect_timeout", "tls_error", "dns_error", "other"
func RecordHostError(targetHost, errorType string) {
	HostErrors.WithLabelValues(targetHost, errorType).Inc()
}

// RecordBlockedRequest increments the blocked request counter.
func RecordBlockedRequest() {
	BlockedRequests.Inc()
}
