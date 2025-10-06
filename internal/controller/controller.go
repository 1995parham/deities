package controller

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/1995parham/deities/internal/config"
	"github.com/1995parham/deities/internal/k8s"
	"github.com/1995parham/deities/internal/registry"
)

// Controller manages the image update monitoring and deployment rollouts
type Controller struct {
	config         *config.Config
	registryClient *registry.Client
	k8sClient      *k8s.Client
	imageDigests   map[string]string // tracks current digests for each repository
	mu             sync.RWMutex
}

// NewController creates a new controller instance
func NewController(cfg *config.Config, k8sClient *k8s.Client) *Controller {
	return &Controller{
		config:         cfg,
		registryClient: registry.NewClient(),
		k8sClient:      k8sClient,
		imageDigests:   make(map[string]string),
	}
}

// Start begins the monitoring loop
func (c *Controller) Start(ctx context.Context) error {
	log.Println("Starting Deities controller...")

	// Initial check to populate digests
	if err := c.checkAndUpdate(ctx); err != nil {
		log.Printf("Initial check error: %v", err)
	}

	ticker := time.NewTicker(c.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Controller stopping...")
			return ctx.Err()
		case <-ticker.C:
			if err := c.checkAndUpdate(ctx); err != nil {
				log.Printf("Check and update error: %v", err)
			}
		}
	}
}

// checkAndUpdate checks all repositories for updates and triggers deployments
func (c *Controller) checkAndUpdate(ctx context.Context) error {
	log.Println("Checking for image updates...")

	for _, repo := range c.config.Repositories {
		if err := c.checkRepository(ctx, &repo); err != nil {
			log.Printf("Error checking repository %s: %v", repo.Name, err)
			continue
		}
	}

	return nil
}

// checkRepository checks a single repository for updates
func (c *Controller) checkRepository(ctx context.Context, repo *config.Repository) error {
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
		log.Printf("Initial digest for %s: %s", repoKey, newDigest)
		c.mu.Lock()
		c.imageDigests[repoKey] = newDigest
		c.mu.Unlock()
		return nil
	}

	if oldDigest != newDigest {
		log.Printf("Digest changed for %s: %s -> %s", repoKey, oldDigest, newDigest)

		// Update stored digest
		c.mu.Lock()
		c.imageDigests[repoKey] = newDigest
		c.mu.Unlock()

		// Find and update matching deployments
		if err := c.updateMatchingDeployments(ctx, repo, newDigest); err != nil {
			return fmt.Errorf("failed to update deployments: %w", err)
		}
	} else {
		log.Printf("No change for %s (digest: %s)", repoKey, newDigest)
	}

	return nil
}

// updateMatchingDeployments updates all deployments that use the given repository
func (c *Controller) updateMatchingDeployments(ctx context.Context, repo *config.Repository, newDigest string) error {
	imagePrefix := repo.Image
	if repo.Registry != "" && repo.Registry != "https://registry-1.docker.io" {
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

		log.Printf("Updating deployment %s/%s...", deployment.Namespace, deployment.Name)

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
			log.Printf("Failed to update deployment %s/%s: %v", deployment.Namespace, deployment.Name, err)
			continue
		}

		log.Printf("Successfully updated deployment %s/%s with image %s",
			deployment.Namespace, deployment.Name, newImageRef)
	}

	return nil
}
