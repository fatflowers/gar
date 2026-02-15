package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	defaultProviderName       = "anthropic"
	defaultAnthropicModel     = "claude-sonnet-4-20250514"
	defaultAnthropicVersion   = "2023-06-01"
	defaultRetryMaxRetries    = 3
	defaultRetryBaseDelay     = "300ms"
	defaultRetryMaxDelay      = "5s"
	defaultAgentMaxTurns      = 50
	defaultAgentThinkingLevel = "medium"
	defaultTUITheme           = "dark"
	defaultTUIShowInspector   = true
	defaultConfigRelativePath = ".config/gar/config.toml"
	envProviderDefault        = "GAR_PROVIDER_DEFAULT"
	envAnthropicAPIKey        = "ANTHROPIC_API_KEY"
	envAnthropicModel         = "GAR_ANTHROPIC_MODEL"
	envAnthropicBaseURL       = "GAR_ANTHROPIC_BASE_URL"
	envAnthropicVersion       = "GAR_ANTHROPIC_VERSION"
	envRetryMaxRetries        = "GAR_ANTHROPIC_RETRY_MAX_RETRIES"
	envRetryBaseDelay         = "GAR_ANTHROPIC_RETRY_BASE_DELAY"
	envRetryMaxDelay          = "GAR_ANTHROPIC_RETRY_MAX_DELAY"
)

var (
	// ErrInvalidConfig indicates malformed configuration input.
	ErrInvalidConfig = errors.New("invalid config")
)

// Config is the application configuration root.
type Config struct {
	Provider ProviderConfig `toml:"provider"`
	Agent    AgentConfig    `toml:"agent"`
	TUI      TUIConfig      `toml:"tui"`
}

// ProviderConfig configures model providers.
type ProviderConfig struct {
	Default   string                  `toml:"default"`
	Anthropic AnthropicProviderConfig `toml:"anthropic"`
}

// AnthropicProviderConfig configures Anthropic-specific runtime values.
type AnthropicProviderConfig struct {
	APIKey  string      `toml:"api_key"`
	Model   string      `toml:"model"`
	BaseURL string      `toml:"base_url"`
	Version string      `toml:"version"`
	Retry   RetryConfig `toml:"retry"`
}

// RetryConfig stores retry policy as config-friendly values.
type RetryConfig struct {
	MaxRetries int    `toml:"max_retries"`
	BaseDelay  string `toml:"base_delay"`
	MaxDelay   string `toml:"max_delay"`
}

// AgentConfig configures agent-level behavior.
type AgentConfig struct {
	AutoApprove   []string `toml:"auto_approve"`
	MaxTurns      int      `toml:"max_turns"`
	ThinkingLevel string   `toml:"thinking_level"`
}

// TUIConfig configures terminal UI defaults.
type TUIConfig struct {
	Theme         string `toml:"theme"`
	ShowInspector bool   `toml:"show_inspector"`
}

// LoadOptions controls config loading behavior.
type LoadOptions struct {
	Path string
}

// AnthropicSettings is a validated Anthropic runtime settings snapshot.
type AnthropicSettings struct {
	APIKey  string
	Model   string
	BaseURL string
	Version string
	Retry   AnthropicRetrySettings
}

// AnthropicRetrySettings is the parsed retry policy.
type AnthropicRetrySettings struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// Default returns application defaults.
func Default() Config {
	return Config{
		Provider: ProviderConfig{
			Default: defaultProviderName,
			Anthropic: AnthropicProviderConfig{
				Model:   defaultAnthropicModel,
				Version: defaultAnthropicVersion,
				Retry: RetryConfig{
					MaxRetries: defaultRetryMaxRetries,
					BaseDelay:  defaultRetryBaseDelay,
					MaxDelay:   defaultRetryMaxDelay,
				},
			},
		},
		Agent: AgentConfig{
			AutoApprove:   []string{"ReadFile"},
			MaxTurns:      defaultAgentMaxTurns,
			ThinkingLevel: defaultAgentThinkingLevel,
		},
		TUI: TUIConfig{
			Theme:         defaultTUITheme,
			ShowInspector: defaultTUIShowInspector,
		},
	}
}

// Load reads config file then applies environment variable overrides.
func Load(opts LoadOptions) (Config, error) {
	cfg := Default()

	path := strings.TrimSpace(opts.Path)
	if path == "" {
		path = defaultConfigPath()
	}

	if err := mergeConfigFile(&cfg, path); err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// AnthropicSettings returns validated settings suitable for runtime wiring.
func (c Config) AnthropicSettings() (AnthropicSettings, error) {
	baseDelay, err := time.ParseDuration(strings.TrimSpace(c.Provider.Anthropic.Retry.BaseDelay))
	if err != nil {
		return AnthropicSettings{}, fmt.Errorf("%w: parse anthropic retry base_delay: %v", ErrInvalidConfig, err)
	}
	maxDelay, err := time.ParseDuration(strings.TrimSpace(c.Provider.Anthropic.Retry.MaxDelay))
	if err != nil {
		return AnthropicSettings{}, fmt.Errorf("%w: parse anthropic retry max_delay: %v", ErrInvalidConfig, err)
	}
	if c.Provider.Anthropic.Retry.MaxRetries < 0 {
		return AnthropicSettings{}, fmt.Errorf("%w: anthropic retry max_retries must be >= 0", ErrInvalidConfig)
	}

	return AnthropicSettings{
		APIKey:  strings.TrimSpace(c.Provider.Anthropic.APIKey),
		Model:   strings.TrimSpace(c.Provider.Anthropic.Model),
		BaseURL: strings.TrimSpace(c.Provider.Anthropic.BaseURL),
		Version: strings.TrimSpace(c.Provider.Anthropic.Version),
		Retry: AnthropicRetrySettings{
			MaxRetries: c.Provider.Anthropic.Retry.MaxRetries,
			BaseDelay:  baseDelay,
			MaxDelay:   maxDelay,
		},
	}, nil
}

func mergeConfigFile(cfg *Config, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config file %s: %w", path, err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}
	return nil
}

func applyEnv(cfg *Config) error {
	if value, ok := os.LookupEnv(envProviderDefault); ok && strings.TrimSpace(value) != "" {
		cfg.Provider.Default = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv(envAnthropicAPIKey); ok {
		cfg.Provider.Anthropic.APIKey = value
	}
	if value, ok := os.LookupEnv(envAnthropicModel); ok && strings.TrimSpace(value) != "" {
		cfg.Provider.Anthropic.Model = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv(envAnthropicBaseURL); ok && strings.TrimSpace(value) != "" {
		cfg.Provider.Anthropic.BaseURL = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv(envAnthropicVersion); ok && strings.TrimSpace(value) != "" {
		cfg.Provider.Anthropic.Version = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv(envRetryMaxRetries); ok && strings.TrimSpace(value) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("%w: parse %s: %v", ErrInvalidConfig, envRetryMaxRetries, err)
		}
		cfg.Provider.Anthropic.Retry.MaxRetries = parsed
	}
	if value, ok := os.LookupEnv(envRetryBaseDelay); ok && strings.TrimSpace(value) != "" {
		cfg.Provider.Anthropic.Retry.BaseDelay = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv(envRetryMaxDelay); ok && strings.TrimSpace(value) != "" {
		cfg.Provider.Anthropic.Retry.MaxDelay = strings.TrimSpace(value)
	}
	return nil
}

func validate(cfg Config) error {
	if strings.TrimSpace(cfg.Provider.Default) == "" {
		return fmt.Errorf("%w: provider.default is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.Provider.Anthropic.Model) == "" {
		return fmt.Errorf("%w: provider.anthropic.model is required", ErrInvalidConfig)
	}
	if _, err := cfg.AnthropicSettings(); err != nil {
		return err
	}
	return nil
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, defaultConfigRelativePath)
}
