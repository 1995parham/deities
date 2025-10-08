package registry

import "fmt"

// Registry represents a Docker registry with its address and authentication.
type Registry struct {
	Name string        `json:"name" koanf:"name"` // Registry address (e.g., "https://registry-1.docker.io")
	Auth *RegistryAuth `json:"auth" koanf:"auth,omitempty"`
}

// Image represents a Docker image to monitor.
type Image struct {
	Name     string `json:"name"     koanf:"name"`     // Image name (e.g., "nginx", "myorg/myapp")
	Registry string `json:"registry" koanf:"registry"` // Reference to registry name
	Tag      string `json:"tag"      koanf:"tag"`      // Image tag (e.g., "latest", "stable")
}

func (img Image) Key() string {
	return fmt.Sprintf("%s/%s:%s", img.Registry, img.Name, img.Tag)
}

func (img Image) String() string {
	return img.Key()
}

// RegistryAuth contains authentication details for private registries.
type RegistryAuth struct {
	Username string `json:"username" koanf:"username"`
	Password string `json:"password" koanf:"password"`
}
