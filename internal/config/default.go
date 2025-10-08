package config

import "time"

const (
	defaultCheckIntervalMinutes = 5
)

// Default return default configuration.
func Default() Config {
	return Config{
		CheckInterval: defaultCheckIntervalMinutes * time.Minute,
		Repositories:  []Repository{},
		Deployments:   []Deployment{},
		Kubeconfig:    "",
	}
}
