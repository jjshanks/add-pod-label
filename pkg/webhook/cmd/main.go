package main

import (
	"fmt"
	"net"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jjshanks/pod-label-webhook/pkg/webhook"
)

var (
	cfgFile  string
	address  string
	logLevel string
	console  bool
	certFile string
	keyFile  string
	rootCmd  = &cobra.Command{
		Use:   "webhook",
		Short: "Kubernetes admission webhook for pod labeling",
		Long:  `A webhook server that adds labels to pods using Kubernetes admission webhooks`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Configure logging
			level, err := zerolog.ParseLevel(logLevel)
			if err != nil {
				return fmt.Errorf("invalid log level %q: %v", logLevel, err)
			}
			zerolog.SetGlobalLevel(level)

			// Configure console output if requested
			if console {
				log.Logger = log.Output(zerolog.ConsoleWriter{
					Out:        os.Stdout,
					TimeFormat: "2006-01-02T15:04:05.000Z",
				})
			}

			// Validate address format
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("invalid address format %q: %v", address, err)
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
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			config := webhook.WebhookConfig{
				CertFile: certFile,
				KeyFile:  keyFile,
				Address:  address,
			}
			return webhook.Run(config)
		},
	}
)

func init() {
	// Configure default zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	cobra.OnInitialize(initConfig)

	// Persistent flags belong to all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.webhook.yaml)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (trace, debug, info, warn, error, fatal, panic)")
	rootCmd.PersistentFlags().BoolVar(&console, "console", false, "Use console log format instead of JSON")

	// Local flags for the root command
	rootCmd.Flags().StringVar(&address, "address", "0.0.0.0:8443", "The address and port to listen on (e.g., 0.0.0.0:8443)")
	rootCmd.Flags().StringVar(&certFile, "cert-file", "/etc/webhook/certs/tls.crt", "Path to the TLS certificate file")
	rootCmd.Flags().StringVar(&keyFile, "key-file", "/etc/webhook/certs/tls.key", "Path to the TLS key file")

	// Bind flags to viper
	if err := viper.BindPFlag("address", rootCmd.Flags().Lookup("address")); err != nil {
		log.Fatal().Err(err).Msg("Error binding address flag")
	}
	if err := viper.BindPFlag("cert-file", rootCmd.Flags().Lookup("cert-file")); err != nil {
		log.Fatal().Err(err).Msg("Error binding cert-file flag")
	}
	if err := viper.BindPFlag("key-file", rootCmd.Flags().Lookup("key-file")); err != nil {
		log.Fatal().Err(err).Msg("Error binding key-file flag")
	}
	if err := viper.BindPFlag("log-level", rootCmd.PersistentFlags().Lookup("log-level")); err != nil {
		log.Fatal().Err(err).Msg("Error binding log-level flag")
	}
	if err := viper.BindPFlag("console", rootCmd.PersistentFlags().Lookup("console")); err != nil {
		log.Fatal().Err(err).Msg("Error binding console flag")
	}

	// Set the environment variable prefix
	viper.SetEnvPrefix("WEBHOOK")

	// Enable environment variable binding
	viper.AutomaticEnv()
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
		// If the specified config file cannot be read, exit with error
		if err := viper.ReadInConfig(); err != nil {
			log.Fatal().Err(err).Msg("Error reading config file")
		}
		log.Info().Str("config", viper.ConfigFileUsed()).Msg("Using config file")
	} else {
		// Search for config in home directory
		home, err := os.UserHomeDir()
		if err != nil {
			log.Error().Err(err).Msg("Error finding home directory")
			return
		}

		// Search config in home directory with name ".webhook" (without extension)
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".webhook")

		// Silently ignore error if default config file is not found
		if err := viper.ReadInConfig(); err == nil {
			log.Info().Str("config", viper.ConfigFileUsed()).Msg("Using config file")
		}
	}

	// Update the values from viper if they're set
	if viper.IsSet("address") {
		address = viper.GetString("address")
	}
	if viper.IsSet("cert-file") {
		certFile = viper.GetString("cert-file")
	}
	if viper.IsSet("key-file") {
		keyFile = viper.GetString("key-file")
	}
	if viper.IsSet("log-level") {
		logLevel = viper.GetString("log-level")
	}
	if viper.IsSet("console") {
		console = viper.GetBool("console")
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Error executing command")
	}
}
