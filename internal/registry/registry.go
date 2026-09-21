package registry

import "fmt"

// Registry represents a Docker registry with its address and authentication.
// Name is the registry address (e.g., "https://registry-1.docker.io"). Auth is
// optional, but when it is present both credentials must be set.
type Registry struct {
	Name string        `json:"name" koanf:"name"           validate:"required"`
	Auth *RegistryAuth `json:"auth" koanf:"auth,omitempty" validate:"omitempty"`
}

// Image represents a Docker image to monitor. Name is the image name (e.g.,
// "nginx", "myorg/myapp") and Registry references the name of a configured
// registry; that reference is resolved by the cross-field validation in
// internal/config.
type Image struct {
	Name     string `json:"name"     koanf:"name"     validate:"required"`
	Registry string `json:"registry" koanf:"registry" validate:"required"`
	Tag      string `json:"tag"      koanf:"tag"      validate:"required"`
}

func (img Image) Key() string {
	return fmt.Sprintf("%s/%s:%s", img.Registry, img.Name, img.Tag)
}

func (img Image) String() string {
	return img.Key()
}

// RegistryAuth contains authentication details for private registries.
type RegistryAuth struct {
	Username string `json:"username" koanf:"username" validate:"required"`
	Password string `json:"password" koanf:"password" validate:"required"`
}
