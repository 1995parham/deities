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

type RegistryNotFoundError struct {
	image    string
	registry string
}

func (err RegistryNotFoundError) Error() string {
	return fmt.Sprintf("registry configuration not found for image %s (registry: %s)", err.image, err.registry)
}

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
	imageDigests   map[string]string             // tracks current digests for each image
	registryMap    map[string]*registry.Registry // maps registry name to registry config
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
	// Build registry map for quick lookup
	registryMap := make(map[string]*registry.Registry)
	for i := range cfg.Registries {
		registryMap[cfg.Registries[i].Name] = &cfg.Registries[i]
	}

	return &Controller{
		config:         cfg,
		registryClient: registryClient,
		k8sClient:      k8sClient,
		imageDigests:   make(map[string]string),
		registryMap:    registryMap,
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

// checkAndUpdate checks all images for updates and triggers deployments.
func (c *Controller) checkAndUpdate(ctx context.Context) {
	c.logger.Info("Checking for image updates...")

	for _, img := range c.config.Images {
		if err := c.checkImage(ctx, &img); err != nil {
			c.logger.Error("Error checking image",
				slog.String("image", img.Name),
				slog.String("tag", img.Tag),
				slog.String("error", err.Error()),
			)

			continue
		}
	}
}

// checkImage checks a single image for updates.
func (c *Controller) checkImage(ctx context.Context, img *registry.Image) error {
	// Resolve registry configuration
	reg, exists := c.registryMap[img.Registry]
	if !exists {
		return RegistryNotFoundError{img.Name, img.Registry}
	}

	imageKey := img.Key()

	// Get current digest from registry
	newDigest, err := c.registryClient.GetImageDigest(ctx, img, reg)
	if err != nil {
		return fmt.Errorf("failed to get digest for %s: %w", imageKey, err)
	}

	c.mu.Lock()
	oldDigest, exists := c.imageDigests[imageKey]
	c.mu.Unlock()

	if !exists {
		// First time seeing this image
		c.logger.Info("Initial digest for image",
			slog.String("image", imageKey),
			slog.String("digest", newDigest),
		)
		c.mu.Lock()
		c.imageDigests[imageKey] = newDigest
		c.mu.Unlock()

		// Check if deployments need to be updated to match the registry
		c.checkDeploymentsOnStartup(ctx, img, reg, newDigest)

		return nil
	}

	if oldDigest != newDigest {
		c.logger.Info("Digest changed for image",
			slog.String("image", imageKey),
			slog.String("old_digest", oldDigest),
			slog.String("new_digest", newDigest),
		)

		// Update stored digest
		c.mu.Lock()
		c.imageDigests[imageKey] = newDigest
		c.mu.Unlock()

		// Find and update matching deployments
		c.updateMatchingDeployments(ctx, img, reg, newDigest)
	} else {
		c.logger.Info("No change for image",
			slog.String("image", imageKey),
			slog.String("digest", newDigest),
		)
	}

	return nil
}

// checkDeploymentsOnStartup checks and updates deployments on initial startup to match registry.
// nolint: funlen
func (c *Controller) checkDeploymentsOnStartup(
	ctx context.Context,
	img *registry.Image,
	reg *registry.Registry,
	registryDigest string,
) {
	const dockerHubRegistry = "https://registry-1.docker.io"

	imagePrefix := img.Name

	if reg.Name != "" && reg.Name != dockerHubRegistry {
		// For non-Docker Hub registries, include registry in comparison
		registryHost := strings.TrimPrefix(reg.Name, "https://")
		registryHost = strings.TrimPrefix(registryHost, "http://")
		imagePrefix = registryHost + "/" + img.Name
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

// updateMatchingDeployments updates all deployments that use the given image.
func (c *Controller) updateMatchingDeployments(
	ctx context.Context,
	img *registry.Image,
	reg *registry.Registry,
	newDigest string,
) {
	const dockerHubRegistry = "https://registry-1.docker.io"

	imagePrefix := img.Name

	if reg.Name != "" && reg.Name != dockerHubRegistry {
		// For non-Docker Hub registries, include registry in comparison
		registryHost := strings.TrimPrefix(reg.Name, "https://")
		registryHost = strings.TrimPrefix(registryHost, "http://")
		imagePrefix = registryHost + "/" + img.Name
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
