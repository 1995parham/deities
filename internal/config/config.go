package config

import (
	"fmt"
	"time"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	defaultCheckIntervalMinutes = 5
)

// Config represents the application configuration.
type Config struct {
	CheckInterval time.Duration `koanf:"check_interval"`
	Repositories  []Repository  `koanf:"repositories"`
	Deployments   []Deployment  `koanf:"deployments"`
	Kubeconfig    string        `koanf:"kubeconfig"`
}

// Repository represents a Docker registry repository to monitor.
type Repository struct {
	Name     string        `koanf:"name"`
	Registry string        `koanf:"registry"`
	Image    string        `koanf:"image"`
	Tag      string        `koanf:"tag"`
	Auth     *RegistryAuth `koanf:"auth,omitempty"`
}

// RegistryAuth contains authentication details for private registries.
type RegistryAuth struct {
	Username string `koanf:"username"`
	Password string `koanf:"password"`
}

// Deployment represents a Kubernetes deployment to manage.
type Deployment struct {
	Name      string `koanf:"name"`
	Namespace string `koanf:"namespace"`
	Container string `koanf:"container"`
	Image     string `koanf:"image"`
}

// Load reads and parses the configuration file.
func Load(path string) (*Config, error) {
	// Create a new koanf instance
	k := koanf.New(".")

	// Load the config file
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set default check interval if not specified
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = defaultCheckIntervalMinutes * time.Minute
	}

	return &cfg, nil
}
