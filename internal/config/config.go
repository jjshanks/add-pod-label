// Package config provides configuration management for the webhook server.
// It handles loading and validation of configuration from multiple sources:
// - Environment variables (prefixed with WEBHOOK_)
// - Configuration files (YAML)
// - Command line flags
//
// Configuration values are loaded with the following precedence (highest to lowest):
// 1. Command line flags
// 2. Environment variables
// 3. Configuration file
// 4. Default values
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Config holds all configuration options for the webhook server.
// All fields can be set via environment variables, config file, or command line flags.
type Config struct {
	// Server configuration
	Address         string        // The address and port to listen on (e.g., "0.0.0.0:8443")
	CertFile        string        // Path to the TLS certificate file
	KeyFile         string        // Path to the TLS private key file
	GracefulTimeout time.Duration // Maximum time to wait for server shutdown

	// Logging configuration
	LogLevel string // Log level (trace, debug, info, warn, error, fatal, panic)
	Console  bool   // Whether to use console-formatted logging instead of JSON
	
	// Tracing configuration
	TracingEnabled      bool   // Whether OpenTelemetry tracing is enabled
	TracingEndpoint     string // OpenTelemetry collector endpoint (e.g., "otel-collector:4317")
	TracingInsecure     bool   // Whether to use insecure connection to the collector
	ServiceNamespace    string // Namespace of the service for resource attribution
	ServiceName         string // Name of the service for resource attribution
	ServiceVersion      string // Version of the service for resource attribution
}

// New creates a new Config with default values.
// These defaults can be overridden by environment variables, config file, or flags.
func New() *Config {
	return &Config{
		// Server defaults
		Address:         "0.0.0.0:8443",
		CertFile:        "/etc/webhook/certs/tls.crt",
		KeyFile:         "/etc/webhook/certs/tls.key",
		GracefulTimeout: 30 * time.Second,
		
		// Logging defaults
		LogLevel:        "info",
		Console:         false,
		
		// Tracing defaults
		TracingEnabled:      false,
		TracingEndpoint:     "",
		TracingInsecure:     false,
		ServiceNamespace:    "default",
		ServiceName:         "pod-label-webhook",
		ServiceVersion:      "dev",
	}
}

// Validate checks if the configuration is valid. It verifies:
// - Log level is a valid zerolog level
// - Address format is valid (host:port)
// - Port is a valid TCP port number
// - Host is a valid IP address (if specified)
// - Graceful timeout is positive
func (c *Config) Validate() error {
	// Validate logging configuration
	if _, err := zerolog.ParseLevel(c.LogLevel); err != nil {
		return fmt.Errorf("invalid log level %q: %v", c.LogLevel, err)
	}

	// Validate address format
	host, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("invalid address format %q: %v", c.Address, err)
	}

	// Validate port
	if _, err := net.LookupPort("tcp", port); err != nil {
		return fmt.Errorf("invalid port %q: %v", port, err)
	}

	// Validate host if specified
	if host != "" && host != "0.0.0.0" {
		if ip := net.ParseIP(host); ip == nil {
			return fmt.Errorf("invalid IP address: %q", host)
		}
	}

	// Validate graceful timeout
	if c.GracefulTimeout <= 0 {
		return fmt.Errorf("graceful timeout must be positive, got %v", c.GracefulTimeout)
	}

	return nil
}

// InitializeLogging sets up the logging configuration based on the Config settings.
// It configures:
// - Global log level
// - Console output format (if enabled)
func (c *Config) InitializeLogging() {
	level, _ := zerolog.ParseLevel(c.LogLevel)
	zerolog.SetGlobalLevel(level)

	if c.Console {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "2006-01-02T15:04:05.000Z",
		})
	}
}

// ValidateCertPaths verifies that the certificate and key files:
// - Exist and are regular files
// - Have appropriate permissions (especially for the private key)
func (c *Config) ValidateCertPaths() error {
	certInfo, err := os.Stat(c.CertFile)
	if err != nil {
		return fmt.Errorf("certificate file error: %v", err)
	}
	if !certInfo.Mode().IsRegular() {
		return fmt.Errorf("certificate path is not a regular file")
	}

	keyInfo, err := os.Stat(c.KeyFile)
	if err != nil {
		return fmt.Errorf("key file error: %v", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return fmt.Errorf("key path is not a regular file")
	}

	// Check key file permissions - should not be readable by group or others
	keyMode := keyInfo.Mode().Perm()
	if keyMode&0o077 != 0 {
		return fmt.Errorf("key file %s has excessive permissions %v", c.KeyFile, keyMode)
	}
	if keyMode > 0o600 {
		log.Warn().Str("key_file", c.KeyFile).Msgf("key file has permissive mode %v", keyMode)
	}
	return nil
}

// LoadConfig loads the configuration from environment variables and the specified config file.
// It follows these steps:
// 1. Set up environment variable binding
// 2. Load config file if specified
// 3. Validate config file values
// 4. Override with environment variables
// 5. Process special cases (like duration parsing)
func LoadConfig(cfgFile string) (*Config, error) {
	config := New()

	// Set up viper for environment variables
	viper.SetEnvPrefix("WEBHOOK")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Bind configuration keys
	configKeys := []string{
		// Server settings
		"address",
		"cert-file",
		"key-file",
		"graceful-timeout",
		
		// Logging settings
		"log-level",
		"console",
		
		// Tracing settings
		"tracing-enabled",
		"tracing-endpoint",
		"tracing-insecure",
		"service-namespace",
		"service-name",
		"service-version",
	}

	// Bind each configuration key to environment variables
	for _, key := range configKeys {
		if err := viper.BindEnv(key); err != nil {
			log.Error().Err(err).Msgf("Failed to bind environment variable for key: %s", key)
		}
	}

	// Load configuration file if specified
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		if err := viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigParseError); ok {
				return nil, fmt.Errorf("error parsing config: %v", err)
			}
			return nil, fmt.Errorf("error reading config file: %v", err)
		}
		log.Info().Str("config", viper.ConfigFileUsed()).Msg("Using config file")

		// Verify types of values in config file
		if viper.IsSet("address") {
			if _, ok := viper.Get("address").(string); !ok {
				return nil, fmt.Errorf("error unmarshaling config: address must be a string")
			}
		}
		if viper.IsSet("cert-file") {
			if _, ok := viper.Get("cert-file").(string); !ok {
				return nil, fmt.Errorf("error unmarshaling config: cert-file must be a string")
			}
		}
		if viper.IsSet("key-file") {
			if _, ok := viper.Get("key-file").(string); !ok {
				return nil, fmt.Errorf("error unmarshaling config: key-file must be a string")
			}
		}
		if viper.IsSet("log-level") {
			if _, ok := viper.Get("log-level").(string); !ok {
				return nil, fmt.Errorf("error unmarshaling config: log-level must be a string")
			}
		}
		if viper.IsSet("console") {
			rawValue := viper.Get("console")
			switch v := rawValue.(type) {
			case bool:
				// This is fine
			case string:
				if _, err := strconv.ParseBool(v); err != nil {
					return nil, fmt.Errorf("error unmarshaling config: console must be a boolean")
				}
			default:
				return nil, fmt.Errorf("error unmarshaling config: console must be a boolean")
			}
		}
		if viper.IsSet("graceful-timeout") {
			rawValue := viper.Get("graceful-timeout")
			switch v := rawValue.(type) {
			case int, int32, int64:
				// Will be handled in the update section
			case string:
				if _, err := time.ParseDuration(v); err != nil {
					return nil, fmt.Errorf("invalid graceful timeout duration: %v", err)
				}
			default:
				return nil, fmt.Errorf("graceful timeout must be a duration string or integer seconds")
			}
		}
	}

	// Update config from viper (environment variables or config file values)
	// Server configuration
	if viper.IsSet("address") {
		config.Address = viper.GetString("address")
	}
	if viper.IsSet("cert-file") {
		config.CertFile = viper.GetString("cert-file")
	}
	if viper.IsSet("key-file") {
		config.KeyFile = viper.GetString("key-file")
	}
	if viper.IsSet("graceful-timeout") {
		rawValue := viper.GetString("graceful-timeout")
		if duration, err := time.ParseDuration(rawValue); err == nil {
			config.GracefulTimeout = duration
		} else if seconds, err := strconv.ParseInt(rawValue, 10, 64); err == nil && seconds > 0 {
			config.GracefulTimeout = time.Duration(seconds) * time.Second
		} else {
			return nil, fmt.Errorf("invalid graceful timeout value: %s (must be duration string or positive integer)", rawValue)
		}
	}
	
	// Logging configuration
	if viper.IsSet("log-level") {
		config.LogLevel = viper.GetString("log-level")
	}
	if viper.IsSet("console") {
		config.Console = viper.GetBool("console")
	}
	
	// Tracing configuration
	if viper.IsSet("tracing-enabled") {
		config.TracingEnabled = viper.GetBool("tracing-enabled")
	}
	if viper.IsSet("tracing-endpoint") {
		config.TracingEndpoint = viper.GetString("tracing-endpoint")
	}
	if viper.IsSet("tracing-insecure") {
		config.TracingInsecure = viper.GetBool("tracing-insecure")
	}
	if viper.IsSet("service-namespace") {
		config.ServiceNamespace = viper.GetString("service-namespace")
	}
	if viper.IsSet("service-name") {
		config.ServiceName = viper.GetString("service-name")
	}
	if viper.IsSet("service-version") {
		config.ServiceVersion = viper.GetString("service-version")
	}

	return config, nil
}
