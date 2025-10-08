package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/1995parham/deities/internal/config"
	"github.com/1995parham/deities/internal/controller"
	"github.com/1995parham/deities/internal/k8s"
)

func main() {
	configPath := flag.String("config", "config.toml", "Path to configuration file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("Failed to load configuration", slog.String("error", err.Error()))

		os.Exit(1)
	}

	logger.Info("Loaded configuration",
		slog.Int("repositories", len(cfg.Repositories)),
		slog.Int("deployments", len(cfg.Deployments)),
	)

	k8sClient, err := k8s.NewClient(cfg.Kubeconfig, logger)
	if err != nil {
		logger.Error("Failed to create Kubernetes client", slog.String("error", err.Error()))

		os.Exit(1)
	}

	ctrl := controller.NewController(cfg, k8sClient, logger)

	ctx, cancel := context.WithCancel(context.Background())

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Start controller
	err = ctrl.Start(ctx)

	// Ensure cleanup happens
	cancel()

	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Controller error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("Deities stopped gracefully")
}
