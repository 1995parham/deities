package controller

import (
	"time"

	"github.com/1995parham/deities/internal/registry"
)

// Config represents controller configuration.
//
// Registries, Images and Deployments may all be empty: a controller with
// nothing to watch is a valid "not configured yet" state, which is what the
// Helm chart ships by default. Anything that *is* present must be complete.
// The cross-field rule that every Images[].Registry resolves to a configured
// registry lives in internal/config, which owns the validator.
type Config struct {
	CheckInterval time.Duration       `json:"check_interval" koanf:"check_interval" validate:"required,gt=0"`
	Registries    []registry.Registry `json:"registries"     koanf:"registries"     validate:"dive"`
	Images        []registry.Image    `json:"images"         koanf:"images"         validate:"dive"`
	Deployments   []Deployment        `json:"deployments"    koanf:"deployments"    validate:"dive"`
}
