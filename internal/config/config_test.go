// pkg/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	cfg := New()
	assert.NotNil(t, cfg)
	assert.Equal(t, "0.0.0.0:8443", cfg.Address)
	assert.Equal(t, "/etc/webhook/certs/tls.crt", cfg.CertFile)
	assert.Equal(t, "/etc/webhook/certs/tls.key", cfg.KeyFile)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.False(t, cfg.Console)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid configuration",
			config: &Config{
				Address:  "0.0.0.0:8443",
				LogLevel: "info",
			},
			wantErr: false,
		},
		{
			name: "invalid log level",
			config: &Config{
				Address:  "0.0.0.0:8443",
				LogLevel: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid log level",
		},
		{
			name: "invalid address format",
			config: &Config{
				Address:  "invalid",
				LogLevel: "info",
			},
			wantErr: true,
			errMsg:  "invalid address format",
		},
		{
			name: "invalid port",
			config: &Config{
				Address:  "0.0.0.0:999999",
				LogLevel: "info",
			},
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name: "invalid IP",
			config: &Config{
				Address:  "256.256.256.256:8443",
				LogLevel: "info",
			},
			wantErr: true,
			errMsg:  "invalid IP address",
		},
		{
			name: "valid localhost address",
			config: &Config{
				Address:  "127.0.0.1:8443",
				LogLevel: "info",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_InitializeLogging(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantLevel zerolog.Level
		console   bool
	}{
		{
			name: "debug level",
			config: &Config{
				LogLevel: "debug",
				Console:  false,
			},
			wantLevel: zerolog.DebugLevel,
		},
		{
			name: "info level with console",
			config: &Config{
				LogLevel: "info",
				Console:  true,
			},
			wantLevel: zerolog.InfoLevel,
			console:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original logger
			origLogger := zerolog.GlobalLevel()
			defer func() {
				zerolog.SetGlobalLevel(origLogger)
			}()

			tt.config.InitializeLogging()
			assert.Equal(t, tt.wantLevel, zerolog.GlobalLevel())
		})
	}
}

func TestConfig_ValidateCertPaths(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test certificate files
	certFile := filepath.Join(tmpDir, "tls.crt")
	keyFile := filepath.Join(tmpDir, "tls.key")

	err = os.WriteFile(certFile, []byte("test-cert"), 0o644)
	require.NoError(t, err)

	err = os.WriteFile(keyFile, []byte("test-key"), 0o600)
	require.NoError(t, err)

	tests := []struct {
		name      string
		config    *Config
		setupFunc func() error
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid paths and permissions",
			config: &Config{
				CertFile: certFile,
				KeyFile:  keyFile,
			},
			wantErr: false,
		},
		{
			name: "nonexistent certificate",
			config: &Config{
				CertFile: "/nonexistent/cert",
				KeyFile:  keyFile,
			},
			wantErr: true,
			errMsg:  "certificate file error",
		},
		{
			name: "nonexistent key",
			config: &Config{
				CertFile: certFile,
				KeyFile:  "/nonexistent/key",
			},
			wantErr: true,
			errMsg:  "key file error",
		},
		{
			name: "key too permissive",
			config: &Config{
				CertFile: certFile,
				KeyFile:  keyFile,
			},
			setupFunc: func() error {
				return os.Chmod(keyFile, 0o644)
			},
			wantErr: true,
			errMsg:  "has excessive permissions",
		},
		{
			name: "key world readable",
			config: &Config{
				CertFile: certFile,
				KeyFile:  keyFile,
			},
			setupFunc: func() error {
				return os.Chmod(keyFile, 0o604)
			},
			wantErr: true,
			errMsg:  "has excessive permissions",
		},
		{
			name: "key world readable and writable",
			config: &Config{
				CertFile: certFile,
				KeyFile:  keyFile,
			},
			setupFunc: func() error {
				return os.Chmod(keyFile, 0o606)
			},
			wantErr: true,
			errMsg:  "has excessive permissions",
		},
		{
			name: "key group and world readable",
			config: &Config{
				CertFile: certFile,
				KeyFile:  keyFile,
			},
			setupFunc: func() error {
				return os.Chmod(keyFile, 0o644)
			},
			wantErr: true,
			errMsg:  "has excessive permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupFunc != nil {
				err := tt.setupFunc()
				require.NoError(t, err)
			}

			err := tt.config.ValidateCertPaths()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test config files
	validConfig := `
address: "127.0.0.1:8443"
cert-file: "/custom/cert/path"
key-file: "/custom/key/path"
log-level: "debug"
console: true
`
	emptyConfig := ``

	invalidTypeConfig := `
address: 8443
cert-file:
log-level: ["debug"]
console: "not-a-bool"
key-file:
`

	malformedConfig := `
address: "127.0.0.1:8443"
cert-file: "/custom/cert/path"
  invalid-indent:
    - this is not valid yaml
key-file: "/custom/key/path"
log-level: "debug"
console: true
`

	// Write test config files
	validConfigFile := filepath.Join(tmpDir, "valid-config.yaml")
	err = os.WriteFile(validConfigFile, []byte(validConfig), 0o644)
	require.NoError(t, err)

	emptyConfigFile := filepath.Join(tmpDir, "empty-config.yaml")
	err = os.WriteFile(emptyConfigFile, []byte(emptyConfig), 0o644)
	require.NoError(t, err)

	invalidTypeConfigFile := filepath.Join(tmpDir, "invalid-type-config.yaml")
	err = os.WriteFile(invalidTypeConfigFile, []byte(invalidTypeConfig), 0o644)
	require.NoError(t, err)

	malformedConfigFile := filepath.Join(tmpDir, "malformed-config.yaml")
	err = os.WriteFile(malformedConfigFile, []byte(malformedConfig), 0o644)
	require.NoError(t, err)

	nonexistentFile := filepath.Join(tmpDir, "nonexistent.yaml")

	tests := []struct {
		name       string
		configFile string
		envVars    map[string]string
		want       *Config
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "load from valid config file",
			configFile: validConfigFile,
			want: &Config{
				Address:  "127.0.0.1:8443",
				CertFile: "/custom/cert/path",
				KeyFile:  "/custom/key/path",
				LogLevel: "debug",
				Console:  true,
			},
		},
		{
			name:       "load defaults",
			configFile: "",
			want:       New(),
		},
		{
			name:       "load from environment",
			configFile: "",
			envVars: map[string]string{
				"WEBHOOK_ADDRESS":   "localhost:8443",
				"WEBHOOK_LOG_LEVEL": "debug",
				"WEBHOOK_CERT_FILE": "/etc/webhook/certs/tls.crt",
				"WEBHOOK_KEY_FILE":  "/etc/webhook/certs/tls.key",
			},
			want: &Config{
				Address:  "localhost:8443",
				CertFile: "/etc/webhook/certs/tls.crt",
				KeyFile:  "/etc/webhook/certs/tls.key",
				LogLevel: "debug",
				Console:  false,
			},
		},
		{
			name:       "empty config file loads defaults",
			configFile: emptyConfigFile,
			want:       New(),
		},
		{
			name:       "nonexistent config file",
			configFile: nonexistentFile,
			wantErr:    true,
			errMsg:     "error reading config file",
		},
		{
			name:       "invalid type in config file",
			configFile: invalidTypeConfigFile,
			wantErr:    true,
			errMsg:     "error unmarshaling config",
		},
		{
			name:       "malformed config file",
			configFile: malformedConfigFile,
			wantErr:    true,
			errMsg:     "error parsing config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper
			viper.Reset()

			// Set environment variables
			for k, v := range tt.envVars {
				err := os.Setenv(k, v)
				require.NoError(t, err)
				defer os.Unsetenv(k)
			}

			// Initialize viper
			InitViper(tt.configFile)

			// Load config
			got, err := LoadConfig(tt.configFile)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want.Address, got.Address)
			assert.Equal(t, tt.want.CertFile, got.CertFile)
			assert.Equal(t, tt.want.KeyFile, got.KeyFile)
			assert.Equal(t, tt.want.LogLevel, got.LogLevel)
			assert.Equal(t, tt.want.Console, got.Console)
		})
	}
}

func TestInitViper(t *testing.T) {
	// Reset viper before test
	viper.Reset()

	// Initialize viper
	InitViper("")

	// Test environment binding
	envVars := map[string]string{
		"WEBHOOK_ADDRESS":   "localhost:8443",
		"WEBHOOK_CERT_FILE": "/custom/cert/path",
		"WEBHOOK_KEY_FILE":  "/custom/key/path",
		"WEBHOOK_LOG_LEVEL": "debug",
		"WEBHOOK_CONSOLE":   "true",
	}

	for k, v := range envVars {
		err := os.Setenv(k, v)
		require.NoError(t, err)
		defer os.Unsetenv(k)
	}

	// Verify that viper can read the environment variables
	assert.Equal(t, "localhost:8443", viper.GetString("address"))
	assert.Equal(t, "/custom/cert/path", viper.GetString("cert-file"))
	assert.Equal(t, "/custom/key/path", viper.GetString("key-file"))
	assert.Equal(t, "debug", viper.GetString("log-level"))
	assert.Equal(t, true, viper.GetBool("console"))
}
