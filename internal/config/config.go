// pkg/config/config.go
package config

import (
	"fmt"
	"net"
	"os"
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
	// Validate certificate file
	certInfo, err := os.Stat(c.CertFile)
	if err != nil {
		return fmt.Errorf("certificate file error: %v", err)
	}
	if !certInfo.Mode().IsRegular() {
		return fmt.Errorf("certificate path is not a regular file")
	}

	// Validate key file
	keyInfo, err := os.Stat(c.KeyFile)
	if err != nil {
		return fmt.Errorf("key file error: %v", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return fmt.Errorf("key path is not a regular file")
	}

	// Check key file permissions
	keyMode := keyInfo.Mode()
	if keyMode.Perm()&0o077 != 0 {
		return fmt.Errorf("key file %s has excessive permissions %v, expected 0600 or more restrictive",
			c.KeyFile, keyMode.Perm())
	}
	if keyMode.Perm() > 0o600 {
		log.Warn().Str("key_file", c.KeyFile).Msgf("key file has permissive mode %v, recommend 0600", keyMode.Perm())
	}

	return nil
}

// LoadConfig loads the configuration from viper
func LoadConfig(cfgFile string) (*Config, error) {
	config := New()

	// Initialize viper for environment variables first
	viper.SetEnvPrefix("WEBHOOK")
	viper.AutomaticEnv()

	// Map environment variables to config fields
	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)

	// Bind environment variables
	viper.BindEnv("address")
	viper.BindEnv("cert-file")
	viper.BindEnv("key-file")
	viper.BindEnv("log-level")
	viper.BindEnv("console")

	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("error reading config file: %v", err)
		}
		log.Info().Str("config", viper.ConfigFileUsed()).Msg("Using config file")
	} else {
		// Search for config in home directory
		home, err := os.UserHomeDir()
		if err != nil {
			log.Error().Err(err).Msg("Error finding home directory")
		} else {
			viper.AddConfigPath(home)
			viper.SetConfigType("yaml")
			viper.SetConfigName(".webhook")

			// Silently ignore error if default config file is not found
			if err := viper.ReadInConfig(); err == nil {
				log.Info().Str("config", viper.ConfigFileUsed()).Msg("Using config file")
			}
		}
	}

	// Update config from viper (will get either environment variables or config file values)
	if viper.IsSet("address") {
		config.Address = viper.GetString("address")
	}
	if viper.IsSet("cert-file") {
		config.CertFile = viper.GetString("cert-file")
	}
	if viper.IsSet("key-file") {
		config.KeyFile = viper.GetString("key-file")
	}
	if viper.IsSet("log-level") {
		config.LogLevel = viper.GetString("log-level")
	}
	if viper.IsSet("console") {
		config.Console = viper.GetBool("console")
	}

	return config, nil
}

// InitViper initializes viper with flags and environment variables
func InitViper(cfgFile string) {
	// Set the environment variable prefix
	viper.SetEnvPrefix("WEBHOOK")

	// Enable environment variable binding with replacer for hyphens
	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)

	// Enable environment variable binding
	viper.AutomaticEnv()

	// Explicitly bind each configuration key
	viper.BindEnv("address")
	viper.BindEnv("cert-file")
	viper.BindEnv("key-file")
	viper.BindEnv("log-level")
	viper.BindEnv("console")
}
