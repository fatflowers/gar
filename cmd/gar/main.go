package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gar/internal/agent"
	"gar/internal/config"
	"gar/internal/llm"
	"gar/internal/tools"
	"gar/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

const defaultRunMaxTokens = 1024

var errUnsupportedProvider = errors.New("unsupported provider")

func main() {
	if err := execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "gar: %v\n", err)
		os.Exit(1)
	}
}

func execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "gar",
		Short: "gar is a minimal TUI coding agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.LoadOptions{Path: strings.TrimSpace(configPath)})
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			provider, model, err := buildProviderFromConfig(cfg)
			if err != nil {
				return fmt.Errorf("build provider: %w", err)
			}

			registry, err := buildToolRegistry()
			if err != nil {
				return fmt.Errorf("build tool registry: %w", err)
			}

			ag, err := agent.New(agent.Config{
				Provider:     provider,
				ToolRegistry: registry,
				MaxTurns:     cfg.Agent.MaxTurns,
			})
			if err != nil {
				return fmt.Errorf("create agent: %w", err)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve cwd: %w", err)
			}

			app := tui.NewApp(tui.AppConfig{
				Version:       "v0.1.0",
				ModelName:     model,
				CWD:           cwd,
				SessionID:     time.Now().UTC().Format("20060102-150405"),
				ThemeName:     cfg.TUI.Theme,
				ShowInspector: cfg.TUI.ShowInspector,
				Runner:        ag,
				MaxTokens:     defaultRunMaxTokens,
				Tools:         buildToolSpecs(),
			})

			program := tea.NewProgram(app, tea.WithAltScreen())
			if _, err := program.Run(); err != nil {
				return fmt.Errorf("run tui: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	return cmd
}

func buildProviderFromConfig(cfg config.Config) (llm.Provider, string, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider.Default)) {
	case "", "anthropic":
		settings, err := cfg.AnthropicSettings()
		if err != nil {
			return nil, "", fmt.Errorf("resolve anthropic settings: %w", err)
		}
		if strings.TrimSpace(settings.APIKey) == "" {
			return nil, "", llm.ErrMissingAPIKey
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

func buildToolRegistry() (*tools.Registry, error) {
	registry := tools.NewRegistry()
	for _, tool := range builtinTools() {
		if err := registry.Register(tool); err != nil {
			return nil, fmt.Errorf("register %s: %w", tool.Name(), err)
		}
	}
	return registry, nil
}

func buildToolSpecs() []llm.ToolSpec {
	builtin := builtinTools()
	specs := make([]llm.ToolSpec, 0, len(builtin))
	for _, tool := range builtin {
		schema := tool.Schema()
		specs = append(specs, llm.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      append(json.RawMessage(nil), schema...),
		})
	}
	return specs
}

func builtinTools() []tools.Tool {
	return []tools.Tool{
		tools.NewReadTool(),
		tools.NewWriteTool(),
		tools.NewEditTool(),
		tools.NewBashTool(),
	}
}
