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

type Deployment struct {
	Name      string `json:"name"      koanf:"name"`
	Namespace string `json:"namespace" koanf:"namespace"`
	Container string `json:"container" koanf:"container"`
	Image     string `json:"image"     koanf:"image"`
}

type Controller struct {
	config         Config
	registryClient *registry.Client
	k8sClient      *k8s.Client
	imageDigests   map[string]string
	registryMap    map[string]*registry.Registry
	mu             sync.RWMutex
	logger         *slog.Logger
}

func NewController(
	cfg Config,
	registryClient *registry.Client,
	k8sClient *k8s.Client,
	logger *slog.Logger,
) *Controller {
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

func Provide(cfg Config, registryClient *registry.Client, k8sClient *k8s.Client, logger *slog.Logger) *Controller {
	return NewController(cfg, registryClient, k8sClient, logger)
}
func (c *Controller) Start(ctx context.Context) error {
	c.logger.Info("Starting Deities controller...")

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

func (c *Controller) checkAndUpdate(ctx context.Context) {
	c.logger.Info("Checking for image updates...")

	var wg sync.WaitGroup
	for i := range c.config.Images {
		wg.Add(1)
		go func(img *registry.Image) {
			defer wg.Done()
			if err := c.checkImage(ctx, img); err != nil {
				c.logger.Error("Error checking image",
					slog.String("image", img.Name),
					slog.String("tag", img.Tag),
					slog.String("error", err.Error()),
				)
			}
		}(&c.config.Images[i])
	}
	wg.Wait()
}

func (c *Controller) checkImage(ctx context.Context, img *registry.Image) error {
	reg, exists := c.registryMap[img.Registry]
	if !exists {
		return RegistryNotFoundError{img.Name, img.Registry}
	}

	imageKey := img.Key()

	newDigest, err := c.registryClient.GetImageDigest(ctx, img, reg)
	if err != nil {
		return fmt.Errorf("failed to get digest for %s: %w", imageKey, err)
	}

	c.mu.Lock()
	oldDigest, exists := c.imageDigests[imageKey]
	c.mu.Unlock()

	if !exists {
		c.logger.Info("Initial digest for image",
			slog.String("image", imageKey),
			slog.String("digest", newDigest),
		)
		c.mu.Lock()
		c.imageDigests[imageKey] = newDigest
		c.mu.Unlock()

		c.syncDeployments(ctx, img, reg, newDigest)

		return nil
	}

	if oldDigest != newDigest {
		c.logger.Info("Digest changed for image",
			slog.String("image", imageKey),
			slog.String("old_digest", oldDigest),
			slog.String("new_digest", newDigest),
		)

		c.mu.Lock()
		c.imageDigests[imageKey] = newDigest
		c.mu.Unlock()

		c.syncDeployments(ctx, img, reg, newDigest)
	} else {
		c.logger.Debug("No change for image",
			slog.String("image", imageKey),
			slog.String("digest", newDigest),
		)
	}

	return nil
}

func (c *Controller) buildImagePrefix(img *registry.Image, reg *registry.Registry) string {
	imagePrefix := img.Name

	registryHost := strings.TrimPrefix(reg.Name, "https://")
	registryHost = strings.TrimPrefix(registryHost, "http://")

	if registryHost != "" && registryHost != "registry-1.docker.io" && registryHost != "index.docker.io" {
		imagePrefix = registryHost + "/" + img.Name
	}

	return imagePrefix
}

func (c *Controller) syncDeployments(
	ctx context.Context,
	img *registry.Image,
	reg *registry.Registry,
	registryDigest string,
) {
	imagePrefix := c.buildImagePrefix(img, reg)

	for _, deployment := range c.config.Deployments {
		if !strings.HasPrefix(deployment.Image, imagePrefix) {
			continue
		}

		c.syncDeployment(ctx, &deployment, registryDigest)
	}
}

func (c *Controller) syncDeployment(ctx context.Context, deployment *Deployment, registryDigest string) {
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
		return
	}

	if strings.HasSuffix(currentImageID, registryDigest) {
		c.logger.Debug("Deployment already running latest digest",
			slog.String("namespace", deployment.Namespace),
			slog.String("deployment", deployment.Name),
			slog.String("digest", registryDigest),
		)
		return
	}

	c.logger.Info("Running image digest differs from registry, triggering restart",
		slog.String("namespace", deployment.Namespace),
		slog.String("deployment", deployment.Name),
		slog.String("current_image_id", currentImageID),
		slog.String("registry_digest", registryDigest),
	)

	if err := c.k8sClient.RolloutRestart(ctx, deployment.Namespace, deployment.Name); err != nil {
		c.logger.Error("Failed to restart deployment",
			slog.String("namespace", deployment.Namespace),
			slog.String("deployment", deployment.Name),
			slog.String("error", err.Error()),
		)
		return
	}

	c.logger.Info("Successfully restarted deployment",
		slog.String("namespace", deployment.Namespace),
		slog.String("deployment", deployment.Name),
	)
}
