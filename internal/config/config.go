// pkg/config/config.go
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

type Config struct {
	// Server configuration
	Address  string
	CertFile string
	KeyFile  string

	// Logging configuration
	LogLevel string
	Console  bool
}

// New creates a new Config with default values
func New() *Config {
	return &Config{
		Address:  "0.0.0.0:8443",
		CertFile: "/etc/webhook/certs/tls.crt",
		KeyFile:  "/etc/webhook/certs/tls.key",
		LogLevel: "info",
		Console:  false,
	}
}

// Validate checks if the configuration is valid
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

	// Validate host
	if host != "" && host != "0.0.0.0" {
		if ip := net.ParseIP(host); ip == nil {
			return fmt.Errorf("invalid IP address: %q", host)
		}
	}

	return nil
}

// InitializeLogging sets up the logging configuration
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

// ValidateCertPaths verifies the certificate and key files
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

	keyMode := keyInfo.Mode().Perm()
	if keyMode&0o077 != 0 {
		return fmt.Errorf("key file %s has excessive permissions %v", c.KeyFile, keyMode)
	}
	if keyMode > 0o600 {
		log.Warn().Str("key_file", c.KeyFile).Msgf("key file has permissive mode %v", keyMode)
	}
	return nil
}

// LoadConfig loads the configuration from viper
func LoadConfig(cfgFile string) (*Config, error) {
	config := New()

	viper.SetEnvPrefix("WEBHOOK")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Bind all config keys at once
	configKeys := []string{"address", "cert-file", "key-file", "log-level", "console"}
	for _, key := range configKeys {
		if err := viper.BindEnv(key); err != nil {
			log.Error().Err(err).Msgf("Failed to bind environment variable for key: %s", key)
		}
	}

	if cfgFile != "" {
		// Use config file from the flag
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
			switch rawValue.(type) {
			case bool:
				// This is fine
			case string:
				if _, err := strconv.ParseBool(rawValue.(string)); err != nil {
					return nil, fmt.Errorf("error unmarshaling config: console must be a boolean")
				}
			default:
				return nil, fmt.Errorf("error unmarshaling config: console must be a boolean")
			}
		}
	}

	// Update config from viper (will get either environment variables or config file values)
	if viper.IsSet("address") {
		address := viper.GetString("address")
		if address != "" {
			config.Address = address
		}
	}
	if viper.IsSet("cert-file") {
		certFile := viper.GetString("cert-file")
		if certFile != "" {
			config.CertFile = certFile
		}
	}
	if viper.IsSet("key-file") {
		keyFile := viper.GetString("key-file")
		if keyFile != "" {
			config.KeyFile = keyFile
		}
	}
	if viper.IsSet("log-level") {
		logLevel := viper.GetString("log-level")
		if logLevel != "" {
			config.LogLevel = logLevel
		}
	}
	if viper.IsSet("console") {
		config.Console = viper.GetBool("console")
	}

	// Validate configuration values after loading
	if config.Address == "" || config.CertFile == "" || config.KeyFile == "" || config.LogLevel == "" {
		return nil, fmt.Errorf("error unmarshaling config: missing required fields")
	}

	return config, nil
}
