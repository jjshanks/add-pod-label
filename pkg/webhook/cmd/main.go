package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/spf13/cobra"

	"github.com/jjshanks/pod-label-webhook/pkg/webhook"
)

var (
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
	rootCmd.Flags().StringVar(&address, "address", "0.0.0.0:8443", "The address and port to listen on (e.g., 0.0.0.0:8443)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
