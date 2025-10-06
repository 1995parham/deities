package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	ErrImagePullPolicyNotAlways = errors.New("container does not have imagePullPolicy set to Always")
	ErrContainerNotFound        = errors.New("container not found in deployment")
)

// Client handles Kubernetes operations.
type Client struct {
	clientset *kubernetes.Clientset
}

// NewClient creates a new Kubernetes client.
func NewClient(kubeconfig string) (*Client, error) {
	var (
		config *rest.Config
		err    error
	)

	if kubeconfig != "" {
		// Use kubeconfig file
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		// Use in-cluster config
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Client{
		clientset: clientset,
	}, nil
}

// GetDeployment retrieves a deployment from Kubernetes.
func (c *Client) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{}) //nolint:exhaustruct
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
	}

	return deployment, nil
}

// UpdateDeploymentImage updates the image of a container in a deployment.
func (c *Client) UpdateDeploymentImage(ctx context.Context, namespace, name, container, newImage string) error {
	deployment, err := c.GetDeployment(ctx, namespace, name)
	if err != nil {
		return err
	}

	// Verify imagePullPolicy is Always
	containerFound := false

	for i, cont := range deployment.Spec.Template.Spec.Containers {
		if cont.Name == container {
			containerFound = true

			if cont.ImagePullPolicy != "Always" {
				return fmt.Errorf("%w: %s", ErrImagePullPolicyNotAlways, container)
			}

			// Update the image with digest
			deployment.Spec.Template.Spec.Containers[i].Image = newImage

			break
		}
	}

	if !containerFound {
		return fmt.Errorf("%w: %s in %s/%s", ErrContainerNotFound, container, namespace, name)
	}

	// Update the deployment
	//nolint:exhaustruct,lll
	_, err = c.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment: %w", err)
	}

	return nil
}

// RolloutRestart triggers a rollout restart of a deployment.
func (c *Client) RolloutRestart(ctx context.Context, namespace, name string) error {
	deployment, err := c.GetDeployment(ctx, namespace, name)
	if err != nil {
		return err
	}

	// Add or update the restart annotation
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}

	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	//nolint:exhaustruct,lll
	_, err = c.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to restart deployment: %w", err)
	}

	return nil
}

// GetCurrentImageDigest extracts the current image and digest from a deployment container.
func (c *Client) GetCurrentImageDigest(ctx context.Context, namespace, name, container string) (string, error) {
	deployment, err := c.GetDeployment(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	for _, cont := range deployment.Spec.Template.Spec.Containers {
		if cont.Name == container {
			return cont.Image, nil
		}
	}

	return "", fmt.Errorf("%w: %s in %s/%s", ErrContainerNotFound, container, namespace, name)
}
