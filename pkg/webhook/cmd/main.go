package main

import (
	"log"

	"github.com/jjshanks/pod-label-webhook/pkg/webhook"
	"github.com/jjshanks/pod-label-webhook/pkg/webhook/config"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if err := config.ValidateConfig(cfg); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	if err := webhook.Run(cfg); err != nil {
		log.Fatalf("Failed to start webhook: %v", err)
	}
}
