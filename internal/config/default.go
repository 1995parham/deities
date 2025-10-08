package config

import (
	"time"

	"go.uber.org/fx"

	"github.com/1995parham/deities/internal/controller"
	"github.com/1995parham/deities/internal/k8s"
	"github.com/1995parham/deities/internal/logger"
	"github.com/1995parham/deities/internal/registry"
)

const (
	defaultCheckIntervalMinutes = 5
)

// Default return default configuration.
func Default() Config {
	return Config{
		Out: fx.Out{},
		Controller: controller.Config{
			CheckInterval: defaultCheckIntervalMinutes * time.Minute,
			Repositories:  []registry.Repository{},
			Deployments:   []controller.Deployment{},
		},
		K8s: k8s.Config{
			Kubeconfig: "",
		},
		Logger: logger.Config{
			Level: "info",
		},
	}
}
