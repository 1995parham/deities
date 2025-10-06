package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1995parham/deities/internal/config"
)

// Client handles Docker registry operations
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new registry client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{},
	}
}

// ManifestResponse represents the registry manifest response
type ManifestResponse struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	Config        ManifestConfig         `json:"config"`
	Layers        []ManifestLayer        `json:"layers"`
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

// GetImageDigest retrieves the digest of an image from the registry
func (c *Client) GetImageDigest(ctx context.Context, repo *config.Repository) (string, error) {
	registry := repo.Registry
	if registry == "" {
		registry = "https://registry-1.docker.io"
	}

	// For Docker Hub, we need to use the v2 API
	imagePath := repo.Image
	if registry == "https://registry-1.docker.io" && !strings.Contains(imagePath, "/") {
		imagePath = "library/" + imagePath
	}

	// Get authentication token if needed
	token, err := c.getAuthToken(ctx, registry, imagePath, repo.Auth)
	if err != nil {
		return "", fmt.Errorf("failed to get auth token: %w", err)
	}

	// Construct manifest URL
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registry, imagePath, repo.Tag)

	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}

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

// getAuthToken retrieves an authentication token for the registry
func (c *Client) getAuthToken(ctx context.Context, registry, image string, auth *config.RegistryAuth) (string, error) {
	// For Docker Hub
	if registry == "https://registry-1.docker.io" {
		authURL := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull", image)

		req, err := http.NewRequestWithContext(ctx, "GET", authURL, nil)
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
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("auth request failed with status %d", resp.StatusCode)
		}

		var tokenResp struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
			return "", err
		}

		return tokenResp.Token, nil
	}

	// For other registries, basic auth might be sufficient
	// This is a simplified implementation
	return "", nil
}
