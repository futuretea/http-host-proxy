package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config represents the configuration for the HTTP Host Proxy
type Config struct {
	// Server
	ProxyListen string `mapstructure:"proxy_listen"`

	// Proxy behavior
	DefaultScheme       string            `mapstructure:"default_scheme"`
	HostSchemeMap       map[string]string `mapstructure:"host_scheme_map"`
	AllowedHosts        []string          `mapstructure:"allowed_hosts"`
	BlockedHosts        []string          `mapstructure:"blocked_hosts"`
	ExtraRequestHeaders map[string]string `mapstructure:"extra_request_headers"`
	MaxRequestBodySize  int64             `mapstructure:"max_request_body_size"`

	// Transport tuning
	MaxIdleConns          int `mapstructure:"max_idle_conns"`
	MaxIdleConnsPerHost   int `mapstructure:"max_idle_conns_per_host"`
	IdleConnTimeout       int `mapstructure:"idle_conn_timeout"`
	DialTimeout           int `mapstructure:"dial_timeout"`
	TLSHandshakeTimeout   int `mapstructure:"tls_handshake_timeout"`
	ResponseHeaderTimeout int `mapstructure:"response_header_timeout"`

	// TLS
	TLSInsecure bool   `mapstructure:"tls_insecure"`
	TLSEnabled  bool   `mapstructure:"tls_enabled"`
	TLSCertFile string `mapstructure:"tls_cert_file"`
	TLSKeyFile  string `mapstructure:"tls_key_file"`

	// Observability
	PprofListen string `mapstructure:"pprof_listen"`
	LogLevel    string `mapstructure:"log_level"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate default scheme
	if c.DefaultScheme != "" && c.DefaultScheme != "http" && c.DefaultScheme != "https" {
		return fmt.Errorf("default_scheme must be 'http' or 'https', got %s", c.DefaultScheme)
	}

	// Validate host scheme map values
	for host, scheme := range c.HostSchemeMap {
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("host_scheme_map value for '%s' must be 'http' or 'https', got %s", host, scheme)
		}
	}

	// Validate listen address is not empty
	if c.ProxyListen == "" {
		return fmt.Errorf("proxy_listen is required")
	}

	// Validate log level
	validLevels := map[string]bool{
		"trace": true,
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
		"panic": true,
	}
	if c.LogLevel != "" && !validLevels[strings.ToLower(c.LogLevel)] {
		return fmt.Errorf("log_level must be one of: trace, debug, info, warn, error, fatal, panic, got %s", c.LogLevel)
	}

	// Validate TLS configuration
	if c.TLSEnabled {
		if c.TLSCertFile == "" {
			return fmt.Errorf("tls_cert_file is required when tls_enabled is true")
		}
		if c.TLSKeyFile == "" {
			return fmt.Errorf("tls_key_file is required when tls_enabled is true")
		}
	}

	return nil
}

// LoadConfig loads configuration from file and environment variables using Viper
// Priority: command-line flags > environment variables > config file > defaults
func LoadConfig(configPath string) (*Config, error) {
	// Use the global viper instance to access bound command-line flags
	v := viper.GetViper()

	// Set configuration file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Configure environment variable support
	// Environment variables use HTTP_HOST_PROXY_ prefix and replace - with _
	v.SetEnvPrefix("HTTP_HOST_PROXY")
	v.AllowEmptyEnv(true)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	// Unmarshal configuration into struct
	config := &Config{}

	// Manually handle host_scheme_map to avoid unmarshal type conflict
	if mapStr := v.GetString("host_scheme_map"); mapStr != "" {
		config.HostSchemeMap = parseKeyValueString(mapStr)
	} else if mapValue := v.GetStringMapString("host_scheme_map"); len(mapValue) > 0 {
		config.HostSchemeMap = mapValue
	}

	// Manually handle extra_request_headers
	if mapStr := v.GetString("extra_request_headers"); mapStr != "" {
		config.ExtraRequestHeaders = parseKeyValueString(mapStr)
	} else if mapValue := v.GetStringMapString("extra_request_headers"); len(mapValue) > 0 {
		config.ExtraRequestHeaders = mapValue
	}

	// Manually handle allowed_hosts (can be string or slice)
	if hostsStr := v.GetString("allowed_hosts"); hostsStr != "" {
		config.AllowedHosts = parseCommaSeparatedList(hostsStr)
	} else if hostsList := v.GetStringSlice("allowed_hosts"); len(hostsList) > 0 {
		config.AllowedHosts = hostsList
	}

	// Manually handle blocked_hosts (can be string or slice)
	if hostsStr := v.GetString("blocked_hosts"); hostsStr != "" {
		config.BlockedHosts = parseCommaSeparatedList(hostsStr)
	} else if hostsList := v.GetStringSlice("blocked_hosts"); len(hostsList) > 0 {
		config.BlockedHosts = hostsList
	}

	// Unmarshal remaining fields
	config.ProxyListen = v.GetString("proxy_listen")
	config.DefaultScheme = v.GetString("default_scheme")
	config.MaxRequestBodySize = v.GetInt64("max_request_body_size")
	config.MaxIdleConns = v.GetInt("max_idle_conns")
	config.MaxIdleConnsPerHost = v.GetInt("max_idle_conns_per_host")
	config.IdleConnTimeout = v.GetInt("idle_conn_timeout")
	config.DialTimeout = v.GetInt("dial_timeout")
	config.TLSHandshakeTimeout = v.GetInt("tls_handshake_timeout")
	config.ResponseHeaderTimeout = v.GetInt("response_header_timeout")
	config.TLSInsecure = v.GetBool("tls_insecure")
	config.TLSEnabled = v.GetBool("tls_enabled")
	config.TLSCertFile = v.GetString("tls_cert_file")
	config.TLSKeyFile = v.GetString("tls_key_file")
	config.PprofListen = v.GetString("pprof_listen")
	config.LogLevel = v.GetString("log_level")

	// Apply defaults for empty values
	applyDefaults(config)

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// applyDefaults applies default values to empty configuration fields
func applyDefaults(config *Config) {
	if config.ProxyListen == "" {
		config.ProxyListen = ":8080"
	}
	if config.DefaultScheme == "" {
		config.DefaultScheme = "http"
	}
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	// TLSInsecure defaults to false for security
	// (zero value is already false, so no action needed)

	// Transport tuning defaults
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 200
	}
	if config.MaxIdleConnsPerHost == 0 {
		config.MaxIdleConnsPerHost = 100
	}
	if config.IdleConnTimeout == 0 {
		config.IdleConnTimeout = 90
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 30
	}
	if config.TLSHandshakeTimeout == 0 {
		config.TLSHandshakeTimeout = 10
	}
	if config.ResponseHeaderTimeout == 0 {
		config.ResponseHeaderTimeout = 30
	}
	if config.MaxRequestBodySize == 0 {
		config.MaxRequestBodySize = 10485760 // 10MB
	}
}

// GetAllowedHostsMap returns the allowed hosts as an O(1) lookup map (lowercase)
func (c *Config) GetAllowedHostsMap() map[string]struct{} {
	m := make(map[string]struct{})
	for _, host := range c.AllowedHosts {
		m[strings.ToLower(strings.TrimSpace(host))] = struct{}{}
	}
	return m
}

// GetBlockedHostsMap returns the blocked hosts as an O(1) lookup map (lowercase)
func (c *Config) GetBlockedHostsMap() map[string]struct{} {
	m := make(map[string]struct{})
	for _, host := range c.BlockedHosts {
		m[strings.ToLower(strings.TrimSpace(host))] = struct{}{}
	}
	return m
}

// GetHostSchemeMap returns the host scheme map with normalized keys (lowercase)
func (c *Config) GetHostSchemeMap() map[string]string {
	m := make(map[string]string)
	for host, scheme := range c.HostSchemeMap {
		m[strings.ToLower(host)] = scheme
	}
	return m
}

// parseKeyValueString parses comma-separated key=value pairs
// Format: "key1=value1,key2=value2"
func parseKeyValueString(s string) map[string]string {
	m := make(map[string]string)
	if s == "" {
		return m
	}
	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" {
				m[key] = value
			}
		}
	}
	return m
}

// parseCommaSeparatedList parses comma-separated values into a slice
func parseCommaSeparatedList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
