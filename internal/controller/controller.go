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

		// Sync deployments to match registry on startup
		c.syncDeployments(ctx, img, reg, newDigest)

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

		// Sync deployments to match new registry digest
		c.syncDeployments(ctx, img, reg, newDigest)
	} else {
		c.logger.Info("No change for image",
			slog.String("image", imageKey),
			slog.String("digest", newDigest),
		)

		// Even if registry hasn't changed, verify deployments are in sync
		c.syncDeployments(ctx, img, reg, newDigest)
	}

	return nil
}

// buildImagePrefix constructs the image prefix for matching deployments.
// For Docker Hub, it returns just the image name.
// For other registries, it includes the registry host.
func (c *Controller) buildImagePrefix(img *registry.Image, reg *registry.Registry) string {
	const dockerHubRegistry = "https://registry-1.docker.io"

	imagePrefix := img.Name

	if reg.Name != "" && reg.Name != dockerHubRegistry {
		// For non-Docker Hub registries, include registry in comparison
		registryHost := strings.TrimPrefix(reg.Name, "https://")
		registryHost = strings.TrimPrefix(registryHost, "http://")
		imagePrefix = registryHost + "/" + img.Name
	}

	return imagePrefix
}

// syncDeployments ensures all deployments using this image are restarted when the registry digest changes.
// This function compares the running pod's image digest with the registry digest and triggers
// a rollout restart if they differ. The deployment spec keeps using the image tag, but
// imagePullPolicy: Always ensures the latest image is pulled.
// nolint: funlen
func (c *Controller) syncDeployments(
	ctx context.Context,
	img *registry.Image,
	reg *registry.Registry,
	registryDigest string,
) {
	imagePrefix := c.buildImagePrefix(img, reg)

	for _, deployment := range c.config.Deployments {
		// Check if this deployment uses this image
		if !strings.HasPrefix(deployment.Image, imagePrefix) {
			continue
		}

		// Get current running image digest from pod status
		currentImageID, err := c.k8sClient.GetCurrentImageDigest(
			ctx,
			deployment.Namespace,
			deployment.Name,
			deployment.Container,
		)
		if err != nil {
			c.logger.Warn("Skipping deployment check, no ready pods available",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
				slog.String("reason", err.Error()),
			)

			continue
		}

		// Check if running pod's digest matches registry digest
		// currentImageID format: docker.io/library/nginx@sha256:abc...
		// registryDigest format: sha256:abc...
		if !strings.HasSuffix(currentImageID, registryDigest) {
			c.logger.Info("Running image digest differs from registry, triggering restart",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
				slog.String("current_image_id", currentImageID),
				slog.String("registry_digest", registryDigest),
			)

			// Trigger rollout restart to pull latest image
			err := c.k8sClient.RolloutRestart(
				ctx,
				deployment.Namespace,
				deployment.Name,
			)
			if err != nil {
				c.logger.Error("Failed to restart deployment",
					slog.String("namespace", deployment.Namespace),
					slog.String("deployment", deployment.Name),
					slog.String("error", err.Error()),
				)

				continue
			}

			c.logger.Info("Successfully restarted deployment",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
			)
		} else {
			c.logger.Debug("Deployment already running latest digest",
				slog.String("namespace", deployment.Namespace),
				slog.String("deployment", deployment.Name),
				slog.String("digest", registryDigest),
			)
		}
	}
}
