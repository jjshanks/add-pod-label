package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestConfig(t *testing.T) (string, func()) {
	// Create a temporary directory for test config files
	tmpDir, err := os.MkdirTemp("", "config-test")
	require.NoError(t, err)

	// Create config directory
	configDir := filepath.Join(tmpDir, "config")
	err = os.Mkdir(configDir, 0755)
	require.NoError(t, err)

	// Return the temp directory and cleanup function
	return tmpDir, func() {
		os.RemoveAll(tmpDir)
	}
}

func writeJSON(t *testing.T, dir, name string, content string) {
	path := filepath.Join(dir, "config", name)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name          string
		env           string
		region        string
		files         map[string]string // filename -> content
		expected      Config
		expectedError bool
	}{
		{
			name: "default config only",
			files: map[string]string{
				"default.json": `{
					"server": {
						"port": 8080,
						"readHeaderTimeout": "10s",
						"writeTimeout": "30s",
						"readTimeout": "30s",
						"idleTimeout": "120s"
					}
				}`,
			},
			expected: Config{
				Server: ServerConfig{
					Port:              8080,
					ReadHeaderTimeout: 10 * time.Second,
					WriteTimeout:      30 * time.Second,
					ReadTimeout:       30 * time.Second,
					IdleTimeout:       120 * time.Second,
				},
			},
		},
		{
			name: "env override",
			env:  "production",
			files: map[string]string{
				"default.json": `{
					"server": {
						"port": 8080
					}
				}`,
				"production.json": `{
					"server": {
						"port": 443
					}
				}`,
			},
			expected: Config{
				Server: ServerConfig{
					Port:              443,
					ReadHeaderTimeout: 10 * time.Second,  // Default value
					WriteTimeout:      30 * time.Second,  // Default value
					ReadTimeout:       30 * time.Second,  // Default value
					IdleTimeout:       120 * time.Second, // Default value
				},
			},
		},
		{
			name:   "region override",
			env:    "production",
			region: "us-west",
			files: map[string]string{
				"default.json": `{
					"server": {
						"port": 8080
					}
				}`,
				"production.json": `{
					"server": {
						"port": 443
					}
				}`,
				"production.us-west.json": `{
					"server": {
						"port": 8443
					}
				}`,
			},
			expected: Config{
				Server: ServerConfig{
					Port:              8443,
					ReadHeaderTimeout: 10 * time.Second,  // Default value
					WriteTimeout:      30 * time.Second,  // Default value
					ReadTimeout:       30 * time.Second,  // Default value
					IdleTimeout:       120 * time.Second, // Default value
				},
			},
		},
		{
			name: "tls config",
			files: map[string]string{
				"default.json": `{
					"server": {
						"port": 8443
					},
					"tls": {
						"certPath": "/etc/certs/server.crt",
						"keyPath": "/etc/certs/server.key"
					},
					"allowedCertPaths": {
						"/etc/certs": true
					}
				}`,
			},
			expected: Config{
				Server: ServerConfig{
					Port:              8443,
					ReadHeaderTimeout: 10 * time.Second,  // Default value
					WriteTimeout:      30 * time.Second,  // Default value
					ReadTimeout:       30 * time.Second,  // Default value
					IdleTimeout:       120 * time.Second, // Default value
				},
				TLS: TLSConfig{
					CertPath: "/etc/certs/server.crt",
					KeyPath:  "/etc/certs/server.key",
				},
				AllowedCertPaths: map[string]bool{
					"/etc/certs": true,
				},
			},
		},
		{
			name: "missing files ok",
			env:  "staging",
			expected: Config{
				Server: ServerConfig{
					Port:              8080,              // Default value
					ReadHeaderTimeout: 10 * time.Second,  // Default value
					WriteTimeout:      30 * time.Second,  // Default value
					ReadTimeout:       30 * time.Second,  // Default value
					IdleTimeout:       120 * time.Second, // Default value
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, cleanup := setupTestConfig(t)
			defer cleanup()

			// Set the working directory to our temp directory
			originalWd, _ := os.Getwd()
			err := os.Chdir(tmpDir)
			require.NoError(t, err)
			defer os.Chdir(originalWd)

			// Write test files
			for name, content := range tt.files {
				writeJSON(t, tmpDir, name, content)
			}

			// Set environment variables
			if tt.env != "" {
				os.Setenv("ENV", tt.env)
				defer os.Unsetenv("ENV")
			}
			if tt.region != "" {
				os.Setenv("REGION", tt.region)
				defer os.Unsetenv("REGION")
			}

			// Load config
			cfg, err := LoadConfig()

			if tt.expectedError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected.Server.Port, cfg.Server.Port)
			assert.Equal(t, tt.expected.Server.ReadHeaderTimeout, cfg.Server.ReadHeaderTimeout)
			assert.Equal(t, tt.expected.Server.WriteTimeout, cfg.Server.WriteTimeout)
			assert.Equal(t, tt.expected.Server.ReadTimeout, cfg.Server.ReadTimeout)
			assert.Equal(t, tt.expected.Server.IdleTimeout, cfg.Server.IdleTimeout)
			assert.Equal(t, tt.expected.TLS.CertPath, cfg.TLS.CertPath)
			assert.Equal(t, tt.expected.TLS.KeyPath, cfg.TLS.KeyPath)
			assert.Equal(t, tt.expected.AllowedCertPaths, cfg.AllowedCertPaths)
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		expectedError bool
	}{
		{
			name: "valid config",
			config: Config{
				Server: ServerConfig{
					Port: 8080,
				},
			},
			expectedError: false,
		},
		{
			name: "invalid port - too low",
			config: Config{
				Server: ServerConfig{
					Port: 0,
				},
			},
			expectedError: true,
		},
		{
			name: "invalid port - too high",
			config: Config{
				Server: ServerConfig{
					Port: 65536,
				},
			},
			expectedError: true,
		},
		{
			name: "valid TLS config",
			config: Config{
				Server: ServerConfig{
					Port: 8443,
				},
				TLS: TLSConfig{
					CertPath: "/etc/certs/server.crt",
					KeyPath:  "/etc/certs/server.key",
				},
				AllowedCertPaths: map[string]bool{
					"/etc/certs": true,
				},
			},
			expectedError: false,
		},
		{
			name: "invalid TLS config - missing cert",
			config: Config{
				Server: ServerConfig{
					Port: 8443,
				},
				TLS: TLSConfig{
					KeyPath: "/etc/certs/server.key",
				},
			},
			expectedError: true,
		},
		{
			name: "invalid TLS config - missing key",
			config: Config{
				Server: ServerConfig{
					Port: 8443,
				},
				TLS: TLSConfig{
					CertPath: "/etc/certs/server.crt",
				},
			},
			expectedError: true,
		},
		{
			name: "invalid TLS config - cert not in allowed paths",
			config: Config{
				Server: ServerConfig{
					Port: 8443,
				},
				TLS: TLSConfig{
					CertPath: "/etc/unauthorized/server.crt",
					KeyPath:  "/etc/unauthorized/server.key",
				},
				AllowedCertPaths: map[string]bool{
					"/etc/certs": true,
				},
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(&tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
