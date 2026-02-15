package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.Provider.Default != "anthropic" {
		t.Fatalf("Provider.Default = %q, want %q", cfg.Provider.Default, "anthropic")
	}
	if cfg.Provider.Anthropic.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("Provider.Anthropic.Model = %q, want %q", cfg.Provider.Anthropic.Model, "claude-sonnet-4-20250514")
	}
	if cfg.Provider.Anthropic.Retry.MaxRetries != 3 {
		t.Fatalf("Provider.Anthropic.Retry.MaxRetries = %d, want %d", cfg.Provider.Anthropic.Retry.MaxRetries, 3)
	}
	if cfg.Provider.Anthropic.Retry.BaseDelay != "300ms" {
		t.Fatalf("Provider.Anthropic.Retry.BaseDelay = %q, want %q", cfg.Provider.Anthropic.Retry.BaseDelay, "300ms")
	}
	if cfg.Provider.Anthropic.Retry.MaxDelay != "5s" {
		t.Fatalf("Provider.Anthropic.Retry.MaxDelay = %q, want %q", cfg.Provider.Anthropic.Retry.MaxDelay, "5s")
	}
}

func TestLoadFromFileAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[provider]
default = "anthropic"

[provider.anthropic]
api_key = "file-key"
model = "file-model"
base_url = "https://file.example"
version = "2024-01-01"

[provider.anthropic.retry]
max_retries = 9
base_delay = "900ms"
max_delay = "9s"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	t.Setenv("GAR_ANTHROPIC_MODEL", "env-model")
	t.Setenv("GAR_ANTHROPIC_BASE_URL", "https://env.example")
	t.Setenv("GAR_ANTHROPIC_VERSION", "2025-02-02")
	t.Setenv("GAR_ANTHROPIC_RETRY_MAX_RETRIES", "4")
	t.Setenv("GAR_ANTHROPIC_RETRY_BASE_DELAY", "400ms")
	t.Setenv("GAR_ANTHROPIC_RETRY_MAX_DELAY", "4s")

	cfg, err := Load(LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Provider.Anthropic.APIKey != "env-key" {
		t.Fatalf("APIKey = %q, want %q", cfg.Provider.Anthropic.APIKey, "env-key")
	}
	if cfg.Provider.Anthropic.Model != "env-model" {
		t.Fatalf("Model = %q, want %q", cfg.Provider.Anthropic.Model, "env-model")
	}
	if cfg.Provider.Anthropic.BaseURL != "https://env.example" {
		t.Fatalf("BaseURL = %q, want %q", cfg.Provider.Anthropic.BaseURL, "https://env.example")
	}
	if cfg.Provider.Anthropic.Version != "2025-02-02" {
		t.Fatalf("Version = %q, want %q", cfg.Provider.Anthropic.Version, "2025-02-02")
	}
	if cfg.Provider.Anthropic.Retry.MaxRetries != 4 {
		t.Fatalf("MaxRetries = %d, want %d", cfg.Provider.Anthropic.Retry.MaxRetries, 4)
	}
	if cfg.Provider.Anthropic.Retry.BaseDelay != "400ms" {
		t.Fatalf("BaseDelay = %q, want %q", cfg.Provider.Anthropic.Retry.BaseDelay, "400ms")
	}
	if cfg.Provider.Anthropic.Retry.MaxDelay != "4s" {
		t.Fatalf("MaxDelay = %q, want %q", cfg.Provider.Anthropic.Retry.MaxDelay, "4s")
	}
}

func TestAnthropicSettingsParsesRetryDurations(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Provider.Anthropic.APIKey = "test-key"
	cfg.Provider.Anthropic.BaseURL = "https://api.example"
	cfg.Provider.Anthropic.Version = "2023-06-01"
	cfg.Provider.Anthropic.Retry.MaxRetries = 6
	cfg.Provider.Anthropic.Retry.BaseDelay = "650ms"
	cfg.Provider.Anthropic.Retry.MaxDelay = "7s"

	settings, err := cfg.AnthropicSettings()
	if err != nil {
		t.Fatalf("AnthropicSettings() error = %v", err)
	}

	if settings.APIKey != "test-key" {
		t.Fatalf("APIKey = %q, want %q", settings.APIKey, "test-key")
	}
	if settings.Retry.MaxRetries != 6 {
		t.Fatalf("Retry.MaxRetries = %d, want %d", settings.Retry.MaxRetries, 6)
	}
	if settings.Retry.BaseDelay != 650*time.Millisecond {
		t.Fatalf("Retry.BaseDelay = %s, want %s", settings.Retry.BaseDelay, 650*time.Millisecond)
	}
	if settings.Retry.MaxDelay != 7*time.Second {
		t.Fatalf("Retry.MaxDelay = %s, want %s", settings.Retry.MaxDelay, 7*time.Second)
	}
}

func TestAnthropicSettingsRejectsInvalidDuration(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Provider.Anthropic.Retry.BaseDelay = "bad-duration"
	_, err := cfg.AnthropicSettings()
	if err == nil {
		t.Fatalf("expected error for invalid retry base delay")
	}
}
