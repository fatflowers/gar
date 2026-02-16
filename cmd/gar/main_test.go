package main

import (
	"errors"
	"testing"

	"gar/internal/config"
	"gar/internal/llm"
)

func TestBuildProviderFromConfigAnthropic(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKey = "test-key"
	cfg.Provider.Anthropic.Model = "claude-sonnet-4-20250514"
	cfg.Provider.Anthropic.BaseURL = "https://api.example"
	cfg.Provider.Anthropic.Version = "2023-06-01"
	cfg.Provider.Anthropic.Retry.MaxRetries = 7
	cfg.Provider.Anthropic.Retry.BaseDelay = "700ms"
	cfg.Provider.Anthropic.Retry.MaxDelay = "9s"

	provider, model, err := buildProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("buildProviderFromConfig() error = %v", err)
	}
	if provider == nil {
		t.Fatalf("expected provider, got nil")
	}
	if model != "claude-sonnet-4-20250514" {
		t.Fatalf("model = %q, want %q", model, "claude-sonnet-4-20250514")
	}
}

func TestBuildProviderFromConfigUnsupportedProvider(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Provider.Default = "openai"

	_, _, err := buildProviderFromConfig(cfg)
	if !errors.Is(err, errUnsupportedProvider) {
		t.Fatalf("expected errUnsupportedProvider, got %v", err)
	}
}

func TestBuildProviderFromConfigMissingAPIKeyFailsFast(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKey = ""

	_, _, err := buildProviderFromConfig(cfg)
	if !errors.Is(err, llm.ErrMissingAPIKey) {
		t.Fatalf("expected llm.ErrMissingAPIKey, got %v", err)
	}
}

func TestBuildToolRegistryRegistersBuiltins(t *testing.T) {
	t.Parallel()

	registry, err := buildToolRegistry()
	if err != nil {
		t.Fatalf("buildToolRegistry() error = %v", err)
	}

	for _, name := range []string{"read", "write", "edit", "bash"} {
		if _, err := registry.Get(name); err != nil {
			t.Fatalf("registry.Get(%q) error = %v", name, err)
		}
	}
}
