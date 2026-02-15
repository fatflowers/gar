package main

import (
	"errors"
	"fmt"
	"strings"

	"gar/internal/config"
	"gar/internal/llm"
)

var errUnsupportedProvider = errors.New("unsupported provider")

func main() {
	// TODO: wire cobra CLI entrypoint.
}

func buildProviderFromConfig(cfg config.Config) (llm.Provider, string, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider.Default)) {
	case "", "anthropic":
		settings, err := cfg.AnthropicSettings()
		if err != nil {
			return nil, "", fmt.Errorf("resolve anthropic settings: %w", err)
		}

		provider := llm.NewAnthropicProvider(llm.AnthropicConfig{
			APIKey:  settings.APIKey,
			BaseURL: settings.BaseURL,
			Version: settings.Version,
			Retry: llm.RetryPolicy{
				MaxRetries: settings.Retry.MaxRetries,
				BaseDelay:  settings.Retry.BaseDelay,
				MaxDelay:   settings.Retry.MaxDelay,
			},
		})
		return provider, settings.Model, nil
	default:
		return nil, "", fmt.Errorf("%w: %s", errUnsupportedProvider, cfg.Provider.Default)
	}
}
