package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1995parham/deities/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nolint: paralleltest
func TestConfigLoadingOrder(t *testing.T) {
	// Create a temporary directory for the test config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	// Write a test config file with specific values
	configContent := `[logger]
level = "debug"

[controller]
check_interval = "10m"
`

	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

	// Change to the temp directory so the config loader can find config.toml
	originalDir, err := os.Getwd()
	require.NoError(t, err)

	defer func() {
		t.Chdir(originalDir)
	}()

	t.Chdir(tempDir)

	// Set up environment variables (these should have highest priority)
	envVars := map[string]string{
		"deities_logger__level":              "warn",
		"deities_controller__check_interval": "15m",
		// replacing array items is not possible with environment variables.
		// "deities_controller__deployments__0__namespace": "test",
		// "deities_controller__deployments__0__name":      "test",
	}

	for key, value := range envVars {
		t.Setenv(key, value)
	}

	cfg := config.Provide()

	// Verify environment variables override file values
	assert.Equal(t, "warn", cfg.Logger.Level)
	assert.Equal(t, 15*time.Minute, cfg.Controller.CheckInterval)
}
