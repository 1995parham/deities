package registry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type Client struct {
	logger *slog.Logger
}

func NewClient(logger *slog.Logger) *Client {
	return &Client{logger: logger}
}

func Provide(logger *slog.Logger) *Client {
	return NewClient(logger)
}

func (c *Client) GetImageDigest(ctx context.Context, img *Image, reg *Registry) (string, error) {
	imageRef := c.buildImageReference(img, reg)

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("invalid image reference %s: %w", imageRef, err)
	}

	auth := c.buildAuthenticator(reg)

	desc, err := remote.Head(ref, remote.WithAuth(auth), remote.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("failed to get image %s: %w", imageRef, err)
	}

	return desc.Digest.String(), nil
}

func (c *Client) buildImageReference(img *Image, reg *Registry) string {
	registryHost := c.normalizeRegistry(reg.Name)
	imageName := img.Name

	if registryHost == dockerHubHost && !strings.Contains(imageName, "/") {
		imageName = "library/" + imageName
	}

	return fmt.Sprintf("%s/%s:%s", registryHost, imageName, img.Tag)
}

func (c *Client) normalizeRegistry(registry string) string {
	if registry == "" {
		return dockerHubHost
	}

	registry = strings.TrimPrefix(registry, "https://")
	registry = strings.TrimPrefix(registry, "http://")

	if registry == "registry-1.docker.io" {
		return dockerHubHost
	}

	return registry
}

func (c *Client) buildAuthenticator(reg *Registry) authn.Authenticator {
	if reg.Auth == nil || reg.Auth.Username == "" {
		return authn.Anonymous
	}

	return &authn.Basic{
		Username: os.ExpandEnv(reg.Auth.Username),
		Password: os.ExpandEnv(reg.Auth.Password),
	}
}
