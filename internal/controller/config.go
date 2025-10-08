package controller

import (
	"time"

	"github.com/1995parham/deities/internal/registry"
)

// Config represents controller configuration.
type Config struct {
	CheckInterval time.Duration       `json:"check_interval" koanf:"check_interval"`
	Registries    []registry.Registry `json:"registries"     koanf:"registries"`
	Images        []registry.Image    `json:"images"         koanf:"images"`
	Deployments   []Deployment        `json:"deployments"    koanf:"deployments"`
}
