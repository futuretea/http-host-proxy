# http-host-proxy

A high-performance, production-grade transparent HTTP reverse proxy that dynamically forwards requests to backends based on the `Host` header.

## Features

- Host-based dynamic routing (transparent forwarding based on Host header)
- Per-host scheme overrides (host_scheme_map)
- Host access control (allowed/blocked hosts with O(1) lookup)
- Configurable connection pooling, HTTP/2, streaming
- Prometheus metrics at /metrics (requests, latency, bytes, errors, active connections)
- Structured JSON logging with request correlation (X-Request-ID)
- Graceful shutdown with LB-aware readiness probe
- Health (/healthz) and readiness (/readyz) probes
- Hot reload of configuration (hosts, log level)
- Flexible config: CLI flags, environment variables, YAML file
- Request body size limits
- Hop-by-hop header stripping (RFC compliant)
- Multi-arch Docker image (amd64/arm64)

## Quick Start

```bash
# Forward requests based on Host header
http-host-proxy --listen :8080 --scheme http
```

## Configuration

Configuration can be provided via CLI flags, environment variables, or a YAML file.

**Priority:** CLI flags > environment variables > config file > defaults

### CLI Flags

```bash
http-host-proxy \
  --listen :8080 \
  --scheme https \
  --allowed-hosts "api.example.com,web.example.com" \
  --blocked-hosts "internal.example.com" \
  --host-scheme-map "secure.example.com=https,insecure.local=http" \
  --log-level debug
```

### Environment Variables

Use the `HTTP_HOST_PROXY_` prefix:

```bash
export HTTP_HOST_PROXY_PROXY_LISTEN=":8080"
export HTTP_HOST_PROXY_DEFAULT_SCHEME="https"
export HTTP_HOST_PROXY_ALLOWED_HOSTS="api.example.com,web.example.com"
export HTTP_HOST_PROXY_HOST_SCHEME_MAP="secure.example.com=https,insecure.local=http"
```

### YAML Config File

```bash
http-host-proxy --config config.yaml
```

Example `config.yaml`:

```yaml
proxy_listen: ":8080"
default_scheme: "http"

host_scheme_map:
  secure.example.com: "https"
  insecure.local: "http"

allowed_hosts:
  - "api.example.com"
  - "web.example.com"

blocked_hosts:
  - "internal.example.com"

extra_request_headers:
  X-Forwarded-Proto: "https"

max_request_body_size: 10485760

# Transport tuning
max_idle_conns: 200
max_idle_conns_per_host: 100
idle_conn_timeout: 90

# TLS for proxy server
tls_enabled: false
# tls_cert_file: "/path/to/cert.pem"
# tls_key_file: "/path/to/key.pem"

log_level: info
```

## Configuration Reference

| Field | CLI Flag | Env Variable | Default | Description |
|-------|----------|--------------|---------|-------------|
| `proxy_listen` | `--listen` | `HTTP_HOST_PROXY_PROXY_LISTEN` | `:8080` | Proxy listen address |
| `default_scheme` | `--scheme` | `HTTP_HOST_PROXY_DEFAULT_SCHEME` | `http` | Default scheme for proxied requests |
| `host_scheme_map` | `--host-scheme-map` | `HTTP_HOST_PROXY_HOST_SCHEME_MAP` | - | Per-host scheme overrides |
| `allowed_hosts` | `--allowed-hosts` | `HTTP_HOST_PROXY_ALLOWED_HOSTS` | - | Allowlist of hosts (empty = all allowed) |
| `blocked_hosts` | `--blocked-hosts` | `HTTP_HOST_PROXY_BLOCKED_HOSTS` | - | Blocklist of hosts |
| `extra_request_headers` | `--extra-headers` | `HTTP_HOST_PROXY_EXTRA_REQUEST_HEADERS` | - | Headers to add to proxied requests |
| `max_request_body_size` | `--max-body-size` | `HTTP_HOST_PROXY_MAX_REQUEST_BODY_SIZE` | `10485760` | Max request body size in bytes (10MB) |
| `max_idle_conns` | `--max-idle-conns` | `HTTP_HOST_PROXY_MAX_IDLE_CONNS` | `200` | Max idle connections total |
| `max_idle_conns_per_host` | `--max-idle-conns-per-host` | `HTTP_HOST_PROXY_MAX_IDLE_CONNS_PER_HOST` | `100` | Max idle connections per host |
| `idle_conn_timeout` | `--idle-conn-timeout` | `HTTP_HOST_PROXY_IDLE_CONN_TIMEOUT` | `90` | Idle connection timeout (seconds) |
| `dial_timeout` | `--dial-timeout` | `HTTP_HOST_PROXY_DIAL_TIMEOUT` | `30` | Dial timeout (seconds) |
| `tls_handshake_timeout` | `--tls-handshake-timeout` | `HTTP_HOST_PROXY_TLS_HANDSHAKE_TIMEOUT` | `10` | TLS handshake timeout (seconds) |
| `response_header_timeout` | `--response-header-timeout` | `HTTP_HOST_PROXY_RESPONSE_HEADER_TIMEOUT` | `30` | Response header timeout (seconds) |
| `tls_insecure` | `--tls-insecure` | `HTTP_HOST_PROXY_TLS_INSECURE` | `false` | Skip backend TLS verification |
| `tls_enabled` | `--tls-enabled` | `HTTP_HOST_PROXY_TLS_ENABLED` | `false` | Enable TLS for proxy server |
| `tls_cert_file` | `--tls-cert-file` | `HTTP_HOST_PROXY_TLS_CERT_FILE` | - | TLS certificate file path |
| `tls_key_file` | `--tls-key-file` | `HTTP_HOST_PROXY_TLS_KEY_FILE` | - | TLS private key file path |
| `log_level` | `--log-level` | `HTTP_HOST_PROXY_LOG_LEVEL` | `info` | Log level (trace/debug/info/warn/error/fatal/panic) |
| `pprof_listen` | `--pprof-listen` | `HTTP_HOST_PROXY_PPROF_LISTEN` | - | pprof HTTP listen address (e.g., `:6060`) |

## Endpoints

| Endpoint | Port | Description |
|----------|------|-------------|
| `/` | `:8080` | Main proxy endpoint (forwards based on Host header) |
| `/metrics` | `:8080` | Prometheus metrics |
| `/healthz` | `:8080` | Liveness probe |
| `/readyz` | `:8080` | Readiness probe (returns 503 during graceful shutdown) |
| `/debug/pprof/` | `--pprof-listen` | pprof profiling (optional, separate port) |

## Docker

Build and run with the provided multi-stage Dockerfile:

```bash
docker build -t http-host-proxy .
docker run -p 8080:8080 http-host-proxy --scheme http
```

With a config file:

```bash
docker run -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  http-host-proxy --config /app/config.yaml
```

## Development

```bash
# Build
go build -o http-host-proxy .

# Run tests
go test ./...

# Run locally
./http-host-proxy --listen :8080 --scheme http --log-level debug
```

## Graceful Shutdown

On SIGTERM/SIGINT:
1. Readiness probe (`/readyz`) returns 503
2. Waits 10 seconds for load balancers to drain
3. Waits up to 5 minutes for in-flight requests to complete
4. Shuts down cleanly

## License

MIT
