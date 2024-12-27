package main

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/jjshanks/pod-label-webhook/internal/config"
	"github.com/jjshanks/pod-label-webhook/internal/webhook"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "webhook",
		Short: "Kubernetes admission webhook for pod labeling",
		Long:  `A webhook server that adds labels to pods using Kubernetes admission webhooks`,
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

func init() {
	// Configure default zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Persistent flags belong to all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.webhook.yaml)")
	rootCmd.PersistentFlags().String("log-level", "info", "Log level (trace, debug, info, warn, error, fatal, panic)")
	rootCmd.PersistentFlags().Bool("console", false, "Use console log format instead of JSON")

	// Local flags for the root command
	rootCmd.Flags().String("address", "0.0.0.0:8443", "The address and port to listen on (e.g., 0.0.0.0:8443)")
	rootCmd.Flags().String("cert-file", "/etc/webhook/certs/tls.crt", "Path to the TLS certificate file")
	rootCmd.Flags().String("key-file", "/etc/webhook/certs/tls.key", "Path to the TLS key file")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Error executing command")
	}
}
