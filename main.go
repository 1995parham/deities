package main

import (
	"context"
	"log/slog"

	"github.com/1995parham/deities/internal/config"
	"github.com/1995parham/deities/internal/controller"
	"github.com/1995parham/deities/internal/k8s"
	"github.com/1995parham/deities/internal/logger"
	"github.com/1995parham/deities/internal/logo"
	"github.com/1995parham/deities/internal/registry"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func main() {
	logo.Print()

	fx.New(
		fx.Provide(config.Provide),
		fx.Provide(logger.Provide),
		fx.Provide(registry.Provide),
		fx.Provide(k8s.Provide),
		fx.Provide(controller.Provide),
		fx.WithLogger(func(logger *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: logger}
		}),
		fx.Invoke(run),
	).Run()
}

func run(
	lc fx.Lifecycle,
	shutdowner fx.Shutdowner,
	ctrl *controller.Controller,
	logger *slog.Logger,
) {
	ctx, cancel := context.WithCancel(context.Background())

	lc.Append(
		fx.Hook{
			OnStart: func(context.Context) error {
				logger.Info("Starting Deities application")

				go func() {
					if err := ctrl.Start(ctx); err != nil {
						logger.Error("Controller error", slog.String("error", err.Error()))

						if shutdownErr := shutdowner.Shutdown(); shutdownErr != nil {
							logger.Error("Failed to shutdown", slog.String("error", shutdownErr.Error()))
						}
					}
				}()

				return nil
			},
			OnStop: func(context.Context) error {
				logger.Info("Deities stopped gracefully")
				cancel()
				return nil
			},
		},
	)
}
