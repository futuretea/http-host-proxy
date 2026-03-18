package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/futuretea/http-host-proxy/pkg/config"
	"github.com/futuretea/http-host-proxy/pkg/proxy"
)

// ShuttingDown indicates if the server is in the process of shutting down
var ShuttingDown atomic.Bool

// bindFlags binds command-line flags to viper configuration keys
func bindFlags(cmd *cobra.Command) {
	flagBindings := map[string]string{
		"proxy_listen":            "listen",
		"default_scheme":          "scheme",
		"allowed_hosts":           "allowed-hosts",
		"blocked_hosts":           "blocked-hosts",
		"host_scheme_map":         "host-scheme-map",
		"extra_request_headers":   "extra-headers",
		"max_request_body_size":   "max-body-size",
		"max_idle_conns":          "max-idle-conns",
		"max_idle_conns_per_host": "max-idle-conns-per-host",
		"idle_conn_timeout":       "idle-conn-timeout",
		"dial_timeout":            "dial-timeout",
		"tls_handshake_timeout":   "tls-handshake-timeout",
		"response_header_timeout": "response-header-timeout",
		"tls_insecure":            "tls-insecure",
		"tls_enabled":             "tls-enabled",
		"tls_cert_file":           "tls-cert-file",
		"tls_key_file":            "tls-key-file",
		"pprof_listen":            "pprof-listen",
		"log_level":               "log-level",
	}

	for key, flag := range flagBindings {
		viper.BindPFlag(key, cmd.Flags().Lookup(flag))
	}
}

// NewRootCmd creates a new cobra command for the HTTP Host Proxy
func NewRootCmd() *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "http-host-proxy",
		Short: "Host-based HTTP reverse proxy",
		Long: `HTTP Host Proxy is a reverse proxy that routes HTTP requests based on the Host header.

It supports host-based scheme selection, allowed/blocked host lists,
custom request headers, and configurable transport settings.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			bindFlags(cmd)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProxy(cfgFile)
		},
	}

	// Add configuration file flag
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path (supports YAML)")

	// Server configuration flags
	cmd.Flags().String("listen", "", "Listen address (default :8080)")
	cmd.Flags().String("scheme", "", "Default scheme for proxied requests (http or https, default: http)")

	// Proxy behavior flags
	cmd.Flags().String("allowed-hosts", "", "Comma-separated list of allowed hosts")
	cmd.Flags().String("blocked-hosts", "", "Comma-separated list of blocked hosts")
	cmd.Flags().String("host-scheme-map", "", "Host to scheme mapping (e.g., 'example.com=https,api.com=http')")
	cmd.Flags().String("extra-headers", "", "Extra request headers (e.g., 'X-Forwarded-Proto=https,X-Real-IP=auto')")
	cmd.Flags().Int64("max-body-size", 0, "Maximum request body size in bytes (default: 10MB)")

	// Transport tuning flags
	cmd.Flags().Int("max-idle-conns", 0, "Maximum idle connections (default: 200)")
	cmd.Flags().Int("max-idle-conns-per-host", 0, "Maximum idle connections per host (default: 100)")
	cmd.Flags().Int("idle-conn-timeout", 0, "Idle connection timeout in seconds (default: 90)")
	cmd.Flags().Int("dial-timeout", 0, "Dial timeout in seconds (default: 30)")
	cmd.Flags().Int("tls-handshake-timeout", 0, "TLS handshake timeout in seconds (default: 10)")
	cmd.Flags().Int("response-header-timeout", 0, "Response header timeout in seconds (default: 30)")

	// TLS flags
	cmd.Flags().Bool("tls-insecure", false, "Skip TLS certificate verification for backend")
	cmd.Flags().Bool("tls-enabled", false, "Enable TLS/HTTPS for proxy server")
	cmd.Flags().String("tls-cert-file", "", "TLS certificate file path")
	cmd.Flags().String("tls-key-file", "", "TLS private key file path")

	// Observability flags
	cmd.Flags().String("log-level", "info", "Log level (trace, debug, info, warn, error, fatal, panic)")
	cmd.Flags().String("pprof-listen", "", "pprof HTTP listen address (e.g., :6060, disabled if empty)")

	return cmd
}

// runProxy runs the HTTP Host Proxy with the given configuration
func runProxy(cfgFile string) error {
	// Load configuration from file, environment variables, and command-line flags
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logging with JSON format
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	// Parse log level from string
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel // fallback to info
	}
	zerolog.SetGlobalLevel(level)

	// Log startup information
	log.Info().
		Str("listen", cfg.ProxyListen).
		Str("scheme", cfg.DefaultScheme).
		Bool("tls", cfg.TLSEnabled).
		Bool("tls_insecure", cfg.TLSInsecure).
		Str("log_level", cfg.LogLevel).
		Int("max_idle_conns", cfg.MaxIdleConns).
		Int("max_idle_conns_per_host", cfg.MaxIdleConnsPerHost).
		Msg("Starting http-host-proxy")

	// Log allowed hosts if configured
	if len(cfg.AllowedHosts) > 0 {
		log.Info().
			Int("count", len(cfg.AllowedHosts)).
			Strs("hosts", cfg.AllowedHosts).
			Msg("Allowed hosts configured")
	}

	// Log blocked hosts if configured
	if len(cfg.BlockedHosts) > 0 {
		log.Info().
			Int("count", len(cfg.BlockedHosts)).
			Strs("hosts", cfg.BlockedHosts).
			Msg("Blocked hosts configured")
	}

	// Log host scheme map if configured
	if len(cfg.HostSchemeMap) > 0 {
		var mappings []string
		for host, scheme := range cfg.HostSchemeMap {
			mappings = append(mappings, fmt.Sprintf("%s → %s", host, scheme))
		}
		log.Info().
			Int("count", len(mappings)).
			Strs("mappings", mappings).
			Msg("Host scheme mappings configured")
	}

	// Start pprof server on separate port if enabled
	var pprofServer *http.Server
	if cfg.PprofListen != "" {
		pprofMux := http.NewServeMux()
		pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
		pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		pprofMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		pprofServer = &http.Server{
			Addr:    cfg.PprofListen,
			Handler: pprofMux,
		}

		go func() {
			log.Info().
				Str("addr", cfg.PprofListen).
				Msg("pprof server enabled")
			if err := pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("pprof server error")
			}
		}()
	}

	// Create proxy mux with routes
	proxyMux := http.NewServeMux()

	// Prometheus metrics endpoint (no auth required)
	proxyMux.Handle("/metrics", promhttp.Handler())

	// Create the proxy handler
	p, err := proxy.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create proxy: %w", err)
	}

	// Health check endpoints using proxy handlers
	proxyMux.HandleFunc("/healthz", p.HealthHandler)
	proxyMux.HandleFunc("/readyz", p.ReadinessHandler)

	// Main proxy handler
	proxyMux.HandleFunc("/", p.ServeHTTP)

	// Create server with explicit configuration for graceful shutdown
	mainServer := &http.Server{
		Addr:    cfg.ProxyListen,
		Handler: proxyMux,
		// No read/write timeout - large transfers can take a long time
		// Graceful shutdown will handle in-flight requests properly
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Setup config hot reload
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Info().Str("file", e.Name).Msg("Config file changed, reloading...")
		newCfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			log.Error().Err(err).Msg("Failed to reload config, keeping current settings")
			return
		}
		p.UpdateConfig(newCfg)
		log.Info().Msg("Config reloaded successfully")
	})
	if cfgFile != "" {
		viper.WatchConfig()
	}

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if cfg.TLSEnabled {
			log.Info().
				Str("cert", cfg.TLSCertFile).
				Str("key", cfg.TLSKeyFile).
				Msg("Serving HTTPS")
			serverErr <- mainServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			log.Info().Msg("Serving HTTP")
			serverErr <- mainServer.ListenAndServe()
		}
	}()

	// Wait for interrupt signal or server error
	select {
	case sig := <-sigChan:
		log.Info().
			Str("signal", sig.String()).
			Msg("Received shutdown signal, starting graceful shutdown...")

		// Mark as shutting down - readiness checks will fail
		ShuttingDown.Store(true)
		p.SetShuttingDown()
		log.Info().Msg("Marked as not ready, waiting for load balancer to remove backend...")

		// Give load balancers time to detect we're not ready (via readiness probe)
		// This prevents new connections from being established
		time.Sleep(10 * time.Second)

		// Create shutdown context with timeout
		// Generous timeout for large transfers
		shutdownTimeout := 5 * time.Minute
		log.Info().
			Dur("timeout", shutdownTimeout).
			Msg("Waiting for active connections to complete")

		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		// Shutdown main server
		if err := mainServer.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Error during main server shutdown")
		} else {
			log.Info().Msg("Main server stopped gracefully")
		}

		// Shutdown pprof server if running
		if pprofServer != nil {
			if err := pprofServer.Shutdown(ctx); err != nil {
				log.Error().Err(err).Msg("Error during pprof server shutdown")
			}
		}

		return nil

	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}

// Execute runs the root command
func Execute() error {
	cmd := NewRootCmd()
	return cmd.Execute()
}
