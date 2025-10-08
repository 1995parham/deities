package registry

// Repository represents a Docker registry repository to monitor.
type Repository struct {
	Name     string        `json:"name"     koanf:"name"`
	Registry string        `json:"registry" koanf:"registry"`
	Image    string        `json:"image"    koanf:"image"`
	Tag      string        `json:"tag"      koanf:"tag"`
	Auth     *RegistryAuth `json:"auth"     koanf:"auth,omitempty"`
}

// RegistryAuth contains authentication details for private registries.
type RegistryAuth struct {
	Username string `json:"username" koanf:"username"`
	Password string `json:"password" koanf:"password"`
}
