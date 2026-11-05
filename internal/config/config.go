// Package config loads, saves, and resolves deptriage configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config contains user-configurable provider defaults and credentials.
type Config struct {
	DefaultProvider string            `toml:"default_provider"`
	DefaultModel    string            `toml:"default_model"`
	Ollama          OllamaConfig      `toml:"ollama"`
	Gemini          ProviderConfig    `toml:"gemini"`
	Groq            ProviderConfig    `toml:"groq"`
	OpenAI          OpenAIConfig      `toml:"openai"`
	Keys            map[string]string `toml:"keys"`
}

// OllamaConfig contains Ollama connection settings.
type OllamaConfig struct {
	Host  string `toml:"host"`
	Model string `toml:"model"`
}

// ProviderConfig contains a provider's default model.
type ProviderConfig struct {
	Model string `toml:"model"`
}

// OpenAIConfig contains settings for OpenAI-compatible APIs.
type OpenAIConfig struct {
	BaseURL string `toml:"base_url"`
	Model   string `toml:"model"`
}

// Defaults returns the built-in configuration values.
func Defaults() Config {
	return Config{
		Ollama: OllamaConfig{Host: "http://localhost:11434", Model: "llama3.2:1b"},
		Gemini: ProviderConfig{Model: "gemini-2.0-flash"},
		Groq:   ProviderConfig{Model: "llama-3.3-70b-versatile"},
		OpenAI: OpenAIConfig{BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
		Keys:   map[string]string{"gemini": "", "groq": "", "openai": ""},
	}
}

// Path returns the configured file path using XDG_CONFIG_HOME or the home fallback.
func Path() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "deptriage", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "deptriage", "config.toml"), nil
}

// Load reads the configuration from path, returning defaults when it is absent.
func Load(path string) (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if cfg.Keys == nil {
		cfg.Keys = map[string]string{}
	}
	return cfg, nil
}

// LoadDefault reads the config at the standard user config path.
func LoadDefault() (Config, string, error) {
	path, err := Path()
	if err != nil {
		return Config{}, "", err
	}
	cfg, err := Load(path)
	return cfg, path, err
}

// Save writes cfg to path with private file permissions.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open config %q: %w", path, err)
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		_ = f.Close()
		return fmt.Errorf("encode config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close config %q: %w", path, err)
	}
	return nil
}

// Resolve returns the first non-empty value in flag, environment, then config order.
func Resolve(flagValue, envValue, configValue string) string {
	for _, value := range []string{flagValue, envValue, configValue} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Redact replaces a secret with a mask and its final four characters.
func Redact(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****" + secret
	}
	return "****" + secret[len(secret)-4:]
}

// Redacted returns a copy of cfg with credentials safe to display.
func (cfg Config) Redacted() Config {
	copy := cfg
	copy.Keys = make(map[string]string, len(cfg.Keys))
	for key, value := range cfg.Keys {
		copy.Keys[key] = Redact(value)
	}
	return copy
}

// Template returns the default config file with explanatory comments.
func Template() string {
	return `# deptriage configuration. Keep this file private; API keys are stored here.
# Provider selection: --provider, DEPTRIAGE_PROVIDER, default_provider, then auto-detection.
default_provider = ""
default_model = ""

[ollama]
host = "http://localhost:11434"
model = "llama3.2:1b"

[gemini]
model = "gemini-2.0-flash"

[groq]
model = "llama-3.3-70b-versatile"

[openai]
base_url = "https://api.openai.com/v1"
model = "gpt-4o-mini"

[keys]
gemini = ""
groq = ""
openai = ""
`
}
