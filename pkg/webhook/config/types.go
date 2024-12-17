package config

import "time"

// ServerConfig holds all server-related configuration
type ServerConfig struct {
	Port              int           `mapstructure:"port"`
	ReadHeaderTimeout time.Duration `mapstructure:"readHeaderTimeout"`
	WriteTimeout      time.Duration `mapstructure:"writeTimeout"`
	ReadTimeout       time.Duration `mapstructure:"readTimeout"`
	IdleTimeout       time.Duration `mapstructure:"idleTimeout"`
}

// TLSConfig holds TLS-related configuration
type TLSConfig struct {
	CertPath string `mapstructure:"certPath"`
	KeyPath  string `mapstructure:"keyPath"`
}

// Config holds the complete configuration structure
type Config struct {
	Server           ServerConfig    `mapstructure:"server"`
	TLS              TLSConfig       `mapstructure:"tls"`
	AllowedCertPaths map[string]bool `mapstructure:"allowedCertPaths"`
}
