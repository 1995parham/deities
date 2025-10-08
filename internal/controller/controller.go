package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/1995parham/deities/internal/k8s"
	"github.com/1995parham/deities/internal/registry"
)

// Deployment represents a Kubernetes deployment to manage.
type Deployment struct {
	Name      string `json:"name"      koanf:"name"`
	Namespace string `json:"namespace" koanf:"namespace"`
	Container string `json:"container" koanf:"container"`
	Image     string `json:"image"     koanf:"image"`
}

// Controller manages the image update monitoring and deployment rollouts.
type Controller struct {
	config         Config
	registryClient *registry.Client
	k8sClient      *k8s.Client
	imageDigests   map[string]string // tracks current digests for each repository
	mu             sync.RWMutex
	logger         *slog.Logger
}

// NewController creates a new controller instance.
func NewController(
	cfg Config,
	registryClient *registry.Client,
	k8sClient *k8s.Client,
	logger *slog.Logger,
) *Controller {
	return &Controller{
		config:         cfg,
		registryClient: registryClient,
		k8sClient:      k8sClient,
		imageDigests:   make(map[string]string),
		mu:             sync.RWMutex{},
		logger:         logger,
	}
}

// Provide creates a new controller instance using fx dependency injection.
func Provide(cfg Config, registryClient *registry.Client, k8sClient *k8s.Client, logger *slog.Logger) *Controller {
	return NewController(cfg, registryClient, k8sClient, logger)
}

// Start begins the monitoring loop.
func (c *Controller) Start(ctx context.Context) error {
	c.logger.Info("Starting Deities controller...")

	// Initial check to populate digests
	c.checkAndUpdate(ctx)

	ticker := time.NewTicker(c.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Controller stopping...")

			return ctx.Err()
		case <-ticker.C:
			c.checkAndUpdate(ctx)
		}
	}
}

// checkAndUpdate checks all repositories for updates and triggers deployments.
func (c *Controller) checkAndUpdate(ctx context.Context) {
	c.logger.Info("Checking for image updates...")

	for _, repo := range c.config.Repositories {
		if err := c.checkRepository(ctx, &repo); err != nil {
			c.logger.Error("Error checking repository",
				slog.String("repository", repo.Name),
				slog.String("error", err.Error()),
			)

			continue
		}
	}
}

// checkRepository checks a single repository for updates.
func (c *Controller) checkRepository(ctx context.Context, repo *registry.Repository) error {
	repoKey := fmt.Sprintf("%s/%s:%s", repo.Registry, repo.Image, repo.Tag)

	// Get current digest from registry
	newDigest, err := c.registryClient.GetImageDigest(ctx, repo)
	if err != nil {
		return fmt.Errorf("failed to get digest for %s: %w", repoKey, err)
	}

	c.mu.Lock()
	oldDigest, exists := c.imageDigests[repoKey]
	c.mu.Unlock()

	if !exists {
		// First time seeing this repository
		c.logger.Info("Initial digest for repository",
			slog.String("repository", repoKey),
			slog.String("digest", newDigest),
		)
		c.mu.Lock()
		c.imageDigests[repoKey] = newDigest
		c.mu.Unlock()

		// Check if deployments need to be updated to match the registry
		c.checkDeploymentsOnStartup(ctx, repo, newDigest)

		return nil
	}

	if oldDigest != newDigest {
		c.logger.Info("Digest changed for repository",
			slog.String("repository", repoKey),
			slog.String("old_digest", oldDigest),
			slog.String("new_digest", newDigest),
		)

		// Update stored digest
		c.mu.Lock()
		c.imageDigests[repoKey] = newDigest
		c.mu.Unlock()

		// Find and update matching deployments
		c.updateMatchingDeployments(ctx, repo, newDigest)
	} else {
		c.logger.Info("No change for repository",
			slog.String("repository", repoKey),
			slog.String("digest", newDigest),
		)
	}

	return nil
}

// checkDeploymentsOnStartup checks and updates deployments on initial startup to match registry.
// nolint: funlen
func (c *Controller) checkDeploymentsOnStartup(ctx context.Context, repo *registry.Repository, registryDigest string) {
	const dockerHubRegistry = "https://registry-1.docker.io"

	imagePrefix := repo.Image

	if repo.Registry != "" && repo.Registry != dockerHubRegistry {
		// For non-Docker Hub registries, include registry in comparison
		registryHost := strings.TrimPrefix(repo.Registry, "https://")
		registryHost = strings.TrimPrefix(registryHost, "http://")
		imagePrefix = registryHost + "/" + repo.Image
	}

	for _, deployment := range c.config.Deployments {
		// Check if this deployment uses this image
		if !strings.HasPrefix(deployment.Image, imagePrefix) {
			continue
		}

		// Get current image from deployment
		currentImage, err := c.k8sClient.GetCurrentImageDigest(
			ctx,
			deployment.Namespace,
			deployment.Name,
			deployment.Container,
		)
		if err != nil {
			c.logger.Error("Failed to get current image for deployment",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
				slog.String("error", err.Error()),
			)

			continue
		}

		c.logger.Info("Deployment current image",
			slog.String("namespace", deployment.Namespace),
			slog.String("deployment", deployment.Name),
			slog.String("image", currentImage),
		)

		// Check if the current image matches the registry digest
		expectedImageRef := fmt.Sprintf("%s@%s", imagePrefix, registryDigest)

		if currentImage != expectedImageRef {
			c.logger.Info("Deployment image mismatch",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
				slog.String("current", currentImage),
				slog.String("expected", expectedImageRef),
			)

			// Update the deployment to match the registry
			err := c.k8sClient.UpdateDeploymentImage(
				ctx,
				deployment.Namespace,
				deployment.Name,
				deployment.Container,
				expectedImageRef,
			)
			if err != nil {
				c.logger.Error("Failed to update deployment on startup",
					slog.String("namespace", deployment.Namespace),
					slog.String("deployment", deployment.Name),
					slog.String("error", err.Error()),
				)

				continue
			}

			c.logger.Info("Updated deployment to match registry digest",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
				slog.String("image", expectedImageRef),
			)
		} else {
			c.logger.Info("Deployment already matches registry digest",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
			)
		}
	}
}

// updateMatchingDeployments updates all deployments that use the given repository.
func (c *Controller) updateMatchingDeployments(ctx context.Context, repo *registry.Repository, newDigest string) {
	const dockerHubRegistry = "https://registry-1.docker.io"

	imagePrefix := repo.Image

	if repo.Registry != "" && repo.Registry != dockerHubRegistry {
		// For non-Docker Hub registries, include registry in comparison
		registryHost := strings.TrimPrefix(repo.Registry, "https://")
		registryHost = strings.TrimPrefix(registryHost, "http://")
		imagePrefix = registryHost + "/" + repo.Image
	}

	for _, deployment := range c.config.Deployments {
		// Check if this deployment uses this image
		if !strings.HasPrefix(deployment.Image, imagePrefix) {
			continue
		}

		c.logger.Info("Updating deployment",
			slog.String("namespace", deployment.Namespace),
			slog.String("deployment", deployment.Name),
		)

		// Construct the new image reference with digest
		newImageRef := fmt.Sprintf("%s@%s", imagePrefix, newDigest)

		// Update the deployment
		err := c.k8sClient.UpdateDeploymentImage(
			ctx,
			deployment.Namespace,
			deployment.Name,
			deployment.Container,
			newImageRef,
		)
		if err != nil {
			c.logger.Error("Failed to update deployment",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
				slog.String("error", err.Error()),
			)

			continue
		}

		c.logger.Info("Successfully updated deployment",
			slog.String("namespace", deployment.Namespace),
			slog.String("deployment", deployment.Name),
			slog.String("image", newImageRef),
		)
	}
}
