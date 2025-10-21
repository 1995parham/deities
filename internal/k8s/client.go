package k8s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Config represents Kubernetes client configuration.
type Config struct {
	Kubeconfig string `json:"kubeconfig" koanf:"kubeconfig"`
}

var (
	ErrImagePullPolicyNotAlways = errors.New("container does not have imagePullPolicy set to Always")
)

// ContainerNotFoundError represents an error when a container is not found in a deployment
// or when no ready running pods are available.
type ContainerNotFoundError struct {
	Container string
	Namespace string
	Name      string
}

func (err ContainerNotFoundError) Error() string {
	return fmt.Sprintf("container %s not found in deployment %s/%s or no ready running pods found", err.Container, err.Namespace, err.Name)
}

// Client handles Kubernetes operations.
type Client struct {
	clientset *kubernetes.Clientset
	logger    *slog.Logger
}

// NewClient creates a new Kubernetes client.
func NewClient(kubeconfig string, logger *slog.Logger) (*Client, error) {
	kubeconfig = os.ExpandEnv(kubeconfig)

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Client{
		clientset: clientset,
		logger:    logger,
	}, nil
}

// Provide creates a new Kubernetes client using fx dependency injection.
func Provide(cfg Config, logger *slog.Logger) (*Client, error) {
	return NewClient(cfg.Kubeconfig, logger)
}

// GetDeployment retrieves a deployment from Kubernetes.
func (c *Client) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{}) //nolint:exhaustruct
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
	}

	return deployment, nil
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

// GetCurrentImageDigest extracts the current running image digest from a deployment container.
// It queries the actual running pods to get the real image digest being used.
// It only considers ready pods in Running phase to avoid checking pods during rollouts or termination.
// Returns an error if no suitable pod is found, allowing the caller to skip this check until the next round.
func (c *Client) GetCurrentImageDigest(ctx context.Context, namespace, name, container string) (string, error) {
	deployment, err := c.GetDeployment(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	// Get pods for this deployment
	labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)
	//nolint:exhaustruct
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for deployment %s/%s: %w", namespace, name, err)
	}

	// Only consider ready pods that are running and not being terminated
	for _, pod := range pods.Items {
		// Skip terminating pods
		if pod.DeletionTimestamp != nil {
			continue
		}

		// Only consider pods in Running phase
		if pod.Status.Phase != "Running" {
			continue
		}

		// Check if pod is ready
		if !isPodReady(&pod) {
			continue
		}

		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.Name == container && containerStatus.Ready {
				// ImageID contains the full image with digest (e.g., docker.io/library/nginx@sha256:...)
				return containerStatus.ImageID, nil
			}
		}
	}

	return "", ContainerNotFoundError{
		Container: container,
		Namespace: namespace,
		Name:      name,
	}
}

// isPodReady checks if a pod is in Ready condition.
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
