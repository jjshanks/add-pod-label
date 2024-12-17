package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jjshanks/pod-label-webhook/pkg/webhook"
)

var (
	cfgFile string
	address string
	rootCmd = &cobra.Command{
		Use:   "webhook",
		Short: "Kubernetes admission webhook for pod labeling",
		Long:  `A webhook server that adds labels to pods using Kubernetes admission webhooks`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
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
			return webhook.Run(address)
		},
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	// Persistent flags belong to all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.webhook.yaml)")

	// Local flags for the root command
	rootCmd.Flags().StringVar(&address, "address", "0.0.0.0:8443", "The address and port to listen on (e.g., 0.0.0.0:8443)")

	// Bind the flag to viper
	viper.BindPFlag("address", rootCmd.Flags().Lookup("address"))

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
			log.Printf("Error reading config file: %v", err)
			os.Exit(1)
		}
		log.Printf("Using config file: %s", viper.ConfigFileUsed())
	} else {
		// Search for config in home directory
		home, err := os.UserHomeDir()
		if err != nil {
			log.Printf("Error finding home directory: %v", err)
			return
		}

		// Search config in home directory with name ".webhook" (without extension)
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".webhook")

		// Silently ignore error if default config file is not found
		if err := viper.ReadInConfig(); err == nil {
			log.Printf("Using config file: %s", viper.ConfigFileUsed())
		}
	}

	// Read in environment variables that match
	viper.AutomaticEnv()

	// Update the address from viper if it's set
	if viper.IsSet("address") {
		address = viper.GetString("address")
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
