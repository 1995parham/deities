package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	CheckInterval time.Duration    `yaml:"check_interval"`
	Repositories  []Repository     `yaml:"repositories"`
	Deployments   []Deployment     `yaml:"deployments"`
	Kubeconfig    string           `yaml:"kubeconfig"`
}

// Repository represents a Docker registry repository to monitor
type Repository struct {
	Name       string            `yaml:"name"`
	Registry   string            `yaml:"registry"`
	Image      string            `yaml:"image"`
	Tag        string            `yaml:"tag"`
	Auth       *RegistryAuth     `yaml:"auth,omitempty"`
}

// RegistryAuth contains authentication details for private registries
type RegistryAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Deployment represents a Kubernetes deployment to manage
type Deployment struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Container string `yaml:"container"`
	Image     string `yaml:"image"`
}

// Load reads and parses the configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set default check interval if not specified
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 5 * time.Minute
	}

	return &cfg, nil
}
