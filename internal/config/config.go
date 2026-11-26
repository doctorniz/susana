// Package config loads susana.yaml. Any ${VAR} in the file is replaced with
// the environment variable of that name, so secrets can stay out of the file.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen       string        `yaml:"listen"`
	APIToken     string        `yaml:"api_token"` // bearer token for /api and /mcp; empty = open
	Interval     time.Duration `yaml:"interval"`
	Integrations []Integration `yaml:"integrations"`
	Notifiers    []Notifier    `yaml:"notifiers"`
	LLM          *LLM          `yaml:"llm"`
}

// Integration is one monitored app. Keys other than the named ones are kept
// in Settings and passed to the integration.
type Integration struct {
	Name     string         `yaml:"name"`
	Kind     string         `yaml:"kind"`
	URL      string         `yaml:"url"`
	APIKey   string         `yaml:"api_key"`
	Settings map[string]any `yaml:",inline"`
}

type Notifier struct {
	Kind     string `yaml:"kind"` // discord, telegram, ntfy
	URL      string `yaml:"url"`  // discord webhook URL or ntfy topic URL
	Token    string `yaml:"token"`
	ChatID   string `yaml:"chat_id"`
	MinLevel string `yaml:"min_level"` // warning (default) or critical
}

type LLM struct {
	Provider string `yaml:"provider"` // openai (any OpenAI-compatible API) or anthropic
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse expands env vars, decodes YAML and applies defaults.
func Parse(raw []byte) (*Config, error) {
	c := &Config{Listen: ":8686", Interval: time.Minute}
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(raw))), c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Interval < 10*time.Second {
		return nil, fmt.Errorf("interval must be at least 10s, got %s", c.Interval)
	}
	seen := map[string]bool{}
	for _, in := range c.Integrations {
		if in.Name == "" || in.Kind == "" {
			return nil, fmt.Errorf("every integration needs a name and a kind")
		}
		if seen[in.Name] {
			return nil, fmt.Errorf("duplicate integration name %q", in.Name)
		}
		seen[in.Name] = true
	}
	return c, nil
}
