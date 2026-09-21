package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1995parham/deities/internal/config"
	"github.com/1995parham/deities/internal/controller"
	"github.com/1995parham/deities/internal/k8s"
	"github.com/1995parham/deities/internal/logger"
	"github.com/1995parham/deities/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

const (
	dockerHub = "https://registry-1.docker.io"
	nginx     = "nginx"
)

// valid returns a configuration that passes validation, so that each test case
// can break exactly one thing.
func valid() config.Config {
	return config.Config{
		Out: fx.Out{},
		Controller: controller.Config{
			CheckInterval: 5 * time.Minute,
			Registries:    []registry.Registry{{Name: dockerHub, Auth: nil}},
			Images:        []registry.Image{{Name: nginx, Registry: dockerHub, Tag: "latest"}},
			Deployments: []controller.Deployment{
				{Name: "nginx-deployment", Namespace: "default", Container: nginx, Image: nginx},
			},
		},
		K8s:    k8s.Config{Kubeconfig: ""},
		Logger: logger.Config{Level: "info"},
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	t.Parallel()

	require.NoError(t, valid().Validate())
}

// The Helm chart ships images: [] and deployments: [], so "nothing to watch"
// has to stay a valid state.
func TestValidateAcceptsDefaults(t *testing.T) {
	t.Parallel()

	require.NoError(t, config.Default().Validate())
}

type rejection struct {
	mutate func(cfg *config.Config)
	want   string
}

func rejections() map[string]rejection {
	return map[string]rejection{
		"zero check interval": {
			mutate: func(cfg *config.Config) { cfg.Controller.CheckInterval = 0 },
			want:   "controller.check_interval: is required",
		},
		"negative check interval": {
			mutate: func(cfg *config.Config) { cfg.Controller.CheckInterval = -time.Minute },
			want:   "controller.check_interval: must be greater than 0",
		},
		"unknown log level": {
			mutate: func(cfg *config.Config) { cfg.Logger.Level = "verbose" },
			want:   "logger.level: must be one of [debug, info, warn, error]",
		},
		"image without a tag": {
			mutate: func(cfg *config.Config) { cfg.Controller.Images[0].Tag = "" },
			want:   "controller.images[0].tag: is required",
		},
		"image pointing at an unknown registry": {
			mutate: func(cfg *config.Config) { cfg.Controller.Images[0].Registry = "https://gcr.io" },
			want:   `controller.images[0].registry: refers to registry "https://gcr.io", which is not declared`,
		},
		"duplicate registry": {
			mutate: func(cfg *config.Config) {
				cfg.Controller.Registries = append(cfg.Controller.Registries, registry.Registry{
					Name: dockerHub,
					Auth: nil,
				})
			},
			want: "controller.registries[1].name: is declared more than once",
		},
		"duplicate deployment": {
			mutate: func(cfg *config.Config) {
				cfg.Controller.Deployments = append(cfg.Controller.Deployments, cfg.Controller.Deployments[0])
			},
			want: "controller.deployments[1].name: is declared more than once",
		},
		"namespace that kubernetes would reject": {
			mutate: func(cfg *config.Config) { cfg.Controller.Deployments[0].Namespace = "Production" },
			want:   "controller.deployments[0].namespace: must be a valid Kubernetes name",
		},
		"deployment without a container": {
			mutate: func(cfg *config.Config) { cfg.Controller.Deployments[0].Container = "" },
			want:   "controller.deployments[0].container: is required",
		},
		"registry auth without a password": {
			mutate: func(cfg *config.Config) {
				cfg.Controller.Registries[0].Auth = &registry.RegistryAuth{Username: "me", Password: ""}
			},
			want: "controller.registries[0].auth.password: is required",
		},
	}
}

func TestValidateRejects(t *testing.T) {
	t.Parallel()

	for name, testCase := range rejections() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := valid()
			testCase.mutate(&cfg)

			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.want)
		})
	}
}

// Every violation is reported in one pass, so a single restart is enough to
// find them all.
func TestValidateReportsEveryViolationAtOnce(t *testing.T) {
	t.Parallel()

	cfg := valid()
	cfg.Controller.CheckInterval = 0
	cfg.Logger.Level = "verbose"
	cfg.Controller.Deployments[0].Name = ""

	var validationErr config.ValidationError

	err := cfg.Validate()
	require.ErrorAs(t, err, &validationErr)
	assert.Len(t, validationErr.Violations, 3)
}

// A zero check_interval used to reach time.NewTicker and panic at startup.
//
//nolint:paralleltest // Provide reads config.toml from the working directory.
func TestProvideRejectsZeroCheckInterval(t *testing.T) {
	tempDir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(tempDir, "config.toml"),
		[]byte("[controller]\ncheck_interval = \"0s\"\n"),
		0o600,
	))

	t.Chdir(tempDir)

	_, err := config.Provide()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "controller.check_interval")
}
