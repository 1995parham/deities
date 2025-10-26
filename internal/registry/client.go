package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type RegistryRequestFailedError struct {
	Registry   string
	Image      string
	Tag        string
	StatusCode int
	Body       string
}

func (err RegistryRequestFailedError) Error() string {
	return fmt.Sprintf("registry request failed for %s/%s:%s (status %d): %s",
		err.Registry, err.Image, err.Tag, err.StatusCode, err.Body)
}

type AuthRequestFailedError struct {
	Realm      string
	StatusCode int
}

func (err AuthRequestFailedError) Error() string {
	return fmt.Sprintf("auth request failed for %s (status %d)", err.Realm, err.StatusCode)
}

type Client struct {
	httpClient *http.Client
	logger     *slog.Logger
}

func NewClient(logger *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
		logger: logger,
	}
}

func Provide(logger *slog.Logger) *Client {
	return NewClient(logger)
}

type manifestResponse struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
}

type authChallenge struct {
	Realm   string
	Service string
	Scope   string
}

func (c *Client) GetImageDigest(ctx context.Context, img *Image, reg *Registry) (string, error) {
	registryAddr := c.normalizeRegistry(reg.Name)
	imagePath := c.normalizeImagePath(registryAddr, img.Name)

	if reg.Auth != nil {
		reg.Auth.Username = os.ExpandEnv(reg.Auth.Username)
		reg.Auth.Password = os.ExpandEnv(reg.Auth.Password)
	}

	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registryAddr, imagePath, img.Tag)

	digest, err := c.fetchManifest(ctx, manifestURL, imagePath, reg.Auth)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest for %s:%s: %w", img.Name, img.Tag, err)
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

func (c *Client) fetchManifest(ctx context.Context, manifestURL, imagePath string, auth *RegistryAuth) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	if auth != nil && auth.Username != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		challenge, err := c.parseAuthChallenge(resp.Header.Get("WWW-Authenticate"))
		if err != nil {
			return "", fmt.Errorf("failed to parse auth challenge: %w", err)
		}

		token, err := c.fetchToken(ctx, challenge, imagePath, auth)
		if err != nil {
			return "", fmt.Errorf("failed to fetch token: %w", err)
		}

		return c.fetchManifestWithToken(ctx, manifestURL, token)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return c.extractDigest(resp)
}

func (c *Client) fetchManifestWithToken(ctx context.Context, manifestURL, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return c.extractDigest(resp)
}

func (c *Client) extractDigest(resp *http.Response) (string, error) {
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest != "" {
		return digest, nil
	}

	var manifest manifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return "", fmt.Errorf("failed to decode manifest: %w", err)
	}

	return manifest.Config.Digest, nil
}

func (c *Client) parseAuthChallenge(header string) (*authChallenge, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, fmt.Errorf("unsupported auth type: %s", header)
	}

	challenge := &authChallenge{}
	re := regexp.MustCompile(`(\w+)="([^"]+)"`)
	matches := re.FindAllStringSubmatch(header, -1)

	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		key, value := match[1], match[2]
		switch key {
		case "realm":
			challenge.Realm = value
		case "service":
			challenge.Service = value
		case "scope":
			challenge.Scope = value
		}
	}

	if challenge.Realm == "" {
		return nil, fmt.Errorf("missing realm in auth challenge")
	}

	return challenge, nil
}

func (c *Client) fetchToken(ctx context.Context, challenge *authChallenge, imagePath string, auth *RegistryAuth) (string, error) {
	tokenURL, err := url.Parse(challenge.Realm)
	if err != nil {
		return "", fmt.Errorf("invalid realm URL: %w", err)
	}

	params := tokenURL.Query()
	if challenge.Service != "" {
		params.Set("service", challenge.Service)
	}
	if challenge.Scope != "" {
		params.Set("scope", challenge.Scope)
	} else {
		params.Set("scope", fmt.Sprintf("repository:%s:pull", imagePath))
	}
	tokenURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
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
		return "", AuthRequestFailedError{
			Realm:      challenge.Realm,
			StatusCode: resp.StatusCode,
		}
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResp.Token != "" {
		return tokenResp.Token, nil
	}
	if tokenResp.AccessToken != "" {
		return tokenResp.AccessToken, nil
	}

	return "", fmt.Errorf("no token in response")
}
