// Package main implements the pod-label-webhook server, which adds labels to Kubernetes pods
// via a mutating admission webhook.
//
// The webhook server supports configuration through environment variables, command line flags,
// and configuration files. It provides health endpoints, metrics, and TLS-secured webhook
// endpoints for pod mutation.
package main

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/jjshanks/pod-label-webhook/internal/config"
	"github.com/jjshanks/pod-label-webhook/internal/webhook"
)

// Configuration variables and root command definition
var (
	// cfgFile holds the path to the configuration file, which can be set via the --config flag
	cfgFile string

	// rootCmd represents the base command when called without any subcommands.
	// It handles configuration loading, server initialization, and execution.
	rootCmd = &cobra.Command{
		Use:   "webhook",
		Short: "Kubernetes admission webhook for pod labeling",
		Long:  `A webhook server that adds labels to pods using Kubernetes admission webhooks`,
		// PreRunE validates the configuration before the server starts.
		// It loads the config file and initializes logging settings.
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig(cfgFile)
			if err != nil {
				return err
			}

			if err := cfg.Validate(); err != nil {
				return err
			}

			cfg.InitializeLogging()
			return nil
		},
		// RunE contains the main server execution logic.
		// It creates and runs the webhook server with the loaded configuration.
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig(cfgFile)
			if err != nil {
				return err
			}
			server, err := webhook.NewServer(cfg)
			if err != nil {
				return err
			}
			return server.Run()
		},
	}
)

// init initializes the cobra command, configures logging, and sets up command line flags.
// It is called automatically when the package is initialized.
func init() {
	// Configure default zerolog timestamp format
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Persistent flags belong to all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.webhook.yaml)")
	rootCmd.PersistentFlags().String("log-level", "info", "Log level (trace, debug, info, warn, error, fatal, panic)")
	rootCmd.PersistentFlags().Bool("console", false, "Use console log format instead of JSON")

	// Local flags for the root command
	rootCmd.Flags().String("address", "0.0.0.0:8443", "The address and port to listen on (e.g., 0.0.0.0:8443)")
	rootCmd.Flags().String("cert-file", "/etc/webhook/certs/tls.crt", "Path to the TLS certificate file")
	rootCmd.Flags().String("key-file", "/etc/webhook/certs/tls.key", "Path to the TLS key file")
	
	// Tracing flags
	rootCmd.Flags().Bool("tracing-enabled", false, "Enable OpenTelemetry tracing")
	rootCmd.Flags().String("tracing-endpoint", "", "OpenTelemetry collector endpoint (e.g., otel-collector:4317)")
	rootCmd.Flags().Bool("tracing-insecure", false, "Use insecure connection to the collector")
	rootCmd.Flags().String("service-namespace", "default", "Namespace of the service for resource attribution")
	rootCmd.Flags().String("service-name", "pod-label-webhook", "Name of the service for resource attribution")
	rootCmd.Flags().String("service-version", "dev", "Version of the service for resource attribution")
}

// main is the entry point for the webhook server.
// It executes the root command and handles any fatal errors that occur during execution.
func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Error executing command")
	}
}
