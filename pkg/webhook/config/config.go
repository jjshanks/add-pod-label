package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// LoadConfig loads the configuration with the following precedence (highest to lowest):
// 1. Environment specific regional config (/config/{env}.{region}.json)
// 2. Environment specific config (/config/{env}.json)
// 3. Default config (/config/default.json)
func LoadConfig() (*Config, error) {
	v := viper.New()

	// Set up Viper defaults
	v.SetConfigType("json")
	v.AddConfigPath("config")

	// Set some reasonable defaults for timeouts if not specified in config files
	v.SetDefault("server.readHeaderTimeout", "10s")
	v.SetDefault("server.writeTimeout", "30s")
	v.SetDefault("server.readTimeout", "30s")
	v.SetDefault("server.idleTimeout", "120s")
	v.SetDefault("server.port", 8080)

	// Load default configuration
	v.SetConfigName("default")
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Only return error if it's not a missing file error
			return nil, fmt.Errorf("error reading default config: %w", err)
		}
	}

	// Get environment from ENV var, default to "development"
	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}

	// Get region from ENV var, default to "default"
	region := os.Getenv("REGION")
	if region == "" {
		region = "default"
	}

	// Try to load environment specific config
	v.SetConfigName(env)
	_ = v.MergeInConfig() // Ignore error if file doesn't exist

	// Try to load region specific config
	regionConfig := fmt.Sprintf("%s.%s", env, region)
	v.SetConfigName(regionConfig)
	_ = v.MergeInConfig() // Ignore error if file doesn't exist

	// Unmarshal the config into our struct
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &config, nil
}

// ValidateConfig performs basic validation of the configuration
func ValidateConfig(cfg *Config) error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid port number: %d", cfg.Server.Port)
	}

	// Validate TLS configuration if cert paths are provided
	if cfg.TLS.CertPath != "" || cfg.TLS.KeyPath != "" {
		if cfg.TLS.CertPath == "" {
			return fmt.Errorf("TLS cert path is required when key path is provided")
		}
		if cfg.TLS.KeyPath == "" {
			return fmt.Errorf("TLS key path is required when cert path is provided")
		}

		// Validate that the cert path is in the allowed paths
		certDir := filepath.Dir(cfg.TLS.CertPath)
		if !cfg.AllowedCertPaths[certDir] {
			return fmt.Errorf("certificate path %s is not in allowed paths", certDir)
		}
	}

	return nil
}
