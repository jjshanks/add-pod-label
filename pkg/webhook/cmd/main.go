package main

import (
	"log"

	"github.com/jjshanks/pod-label-webhook/pkg/webhook"
)

func main() {
	if err := webhook.Run(); err != nil {
		log.Fatalf("Failed to start webhook: %v", err)
	}
}
