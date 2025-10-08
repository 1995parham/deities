package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

var (
	ErrRegistryRequestFailed = errors.New("registry request failed")
	ErrAuthRequestFailed     = errors.New("auth request failed")
)

// Client handles Docker registry operations.
type Client struct {
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a new registry client.
func NewClient(logger *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{}, //nolint:exhaustruct
		logger:     logger,
	}
}

// Provide creates a new registry client using fx dependency injection.
func Provide(logger *slog.Logger) *Client {
	return NewClient(logger)
}

// ManifestResponse represents the registry manifest response.
type ManifestResponse struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Config        ManifestConfig  `json:"config"`
	Layers        []ManifestLayer `json:"layers"`
}

type ManifestConfig struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

type ManifestLayer struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// GetImageDigest retrieves the digest of an image from the registry.
func (c *Client) GetImageDigest(ctx context.Context, img *Image, reg *Registry) (string, error) {
	registryAddr := c.normalizeRegistry(reg.Name)
	imagePath := c.normalizeImagePath(registryAddr, img.Name)

	token, err := c.getAuthToken(ctx, registryAddr, imagePath, reg.Auth)
	if err != nil {
		return "", fmt.Errorf("failed to get auth token: %w", err)
	}

	digest, err := c.fetchManifestDigest(ctx, registryAddr, imagePath, img.Tag, token, reg.Auth)
	if err != nil {
		return "", err
	}

	return digest, nil
}

func (c *Client) normalizeRegistry(registry string) string {
	if registry == "" {
		return dockerHubRegistry
	}

	return registry
}

func (c *Client) normalizeImagePath(registry, image string) string {
	if registry == dockerHubRegistry && !strings.Contains(image, "/") {
		return "library/" + image
	}

	return image
}

func (c *Client) fetchManifestDigest(
	ctx context.Context,
	registry, imagePath, tag, token string,
	auth *RegistryAuth,
) (string, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registry, imagePath, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	// Use bearer token if available (Docker Hub and OAuth2-based registries)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if auth != nil && auth.Username != "" {
		// Fall back to basic auth for registries that support it
		req.SetBasicAuth(auth.Username, auth.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return "", fmt.Errorf("%w: status %d: %s", ErrRegistryRequestFailed, resp.StatusCode, string(body))
	}

	return c.extractDigest(resp)
}

func (c *Client) extractDigest(resp *http.Response) (string, error) {
	// Get digest from Docker-Content-Digest header
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest != "" {
		return digest, nil
	}

	// Fallback: parse the manifest and get config digest
	var manifest ManifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return "", fmt.Errorf("failed to decode manifest: %w", err)
	}

	return manifest.Config.Digest, nil
}

// getAuthToken retrieves an authentication token for the registry.
// Returns a bearer token for Docker Hub (OAuth2), empty string for other registries (will use basic auth).
func (c *Client) getAuthToken(ctx context.Context, registry, image string, auth *RegistryAuth) (string, error) {
	if registry != dockerHubRegistry {
		// For non-Docker Hub registries, return empty token to use basic auth
		// Basic auth will be applied in fetchManifestDigest if credentials are provided
		return "", nil
	}

	return c.getDockerHubToken(ctx, image, auth)
}

func (c *Client) getDockerHubToken(ctx context.Context, image string, auth *RegistryAuth) (string, error) {
	authURL := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull", image)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}

	if auth != nil && auth.Username != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d", ErrAuthRequestFailed, resp.StatusCode)
	}

	var tokenResp struct {
		Token string `json:"token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	return tokenResp.Token, nil
}
