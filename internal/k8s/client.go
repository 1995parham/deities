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

var ErrImagePullPolicyNotAlways = errors.New("container does not have imagePullPolicy set to Always")

type ContainerNotFoundError struct {
	Container string
	Namespace string
	Name      string
}

func (err ContainerNotFoundError) Error() string {
	return fmt.Sprintf(
		"container %s not found in deployment %s/%s or no ready running pods found",
		err.Container,
		err.Namespace,
		err.Name,
	)
}

type Client struct {
	clientset *kubernetes.Clientset
	logger    *slog.Logger
}

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

func Provide(cfg Config, logger *slog.Logger) (*Client, error) {
	return NewClient(cfg.Kubeconfig, logger)
}

func (c *Client) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(
		ctx,
		name,
		metav1.GetOptions{}, // nolint: exhaustruct
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
	}

	return deployment, nil
}

func (c *Client) RolloutRestart(ctx context.Context, namespace, name string) error {
	deployment, err := c.GetDeployment(ctx, namespace, name)
	if err != nil {
		return err
	}

	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}

	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	if _, err := c.clientset.AppsV1().Deployments(namespace).Update(
		ctx,
		deployment,
		metav1.UpdateOptions{}, // nolint: exhaustruct
	); err != nil {
		return fmt.Errorf("failed to restart deployment: %w", err)
	}

	return nil
}

func (c *Client) GetCurrentImageDigest(ctx context.Context, namespace, name, container string) (string, error) {
	deployment, err := c.GetDeployment(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)

	pods, err := c.clientset.CoreV1().Pods(namespace).List(
		ctx,
		metav1.ListOptions{ // nolint: exhaustruct
			LabelSelector: labelSelector,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to list pods for deployment %s/%s: %w", namespace, name, err)
	}

	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || !isPodReady(&pod) {
			continue
		}

		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.Name == container && containerStatus.Ready {
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

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
