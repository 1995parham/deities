package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/1995parham/deities/internal/config"
	"github.com/1995parham/deities/internal/controller"
	"github.com/1995parham/deities/internal/k8s"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Loaded configuration with %d repositories and %d deployments",
		len(cfg.Repositories), len(cfg.Deployments))

	// Create Kubernetes client
	k8sClient, err := k8s.NewClient(cfg.Kubeconfig)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Create controller
	ctrl := controller.NewController(cfg, k8sClient)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		cancel()
	}()

	// Start controller
	err = ctrl.Start(ctx)

	// Ensure cleanup happens
	cancel()

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("Controller error: %v", err)
		os.Exit(1)
	}

	log.Println("Deities stopped gracefully")
}
