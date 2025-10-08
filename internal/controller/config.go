package controller

import (
	"time"

	"github.com/1995parham/deities/internal/registry"
)

// Config represents controller configuration.
type Config struct {
	CheckInterval time.Duration         `json:"check_interval" koanf:"check_interval"`
	Repositories  []registry.Repository `json:"repositories"   koanf:"repositories"`
	Deployments   []Deployment          `json:"deployments"    koanf:"deployments"`
}
