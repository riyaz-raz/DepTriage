package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/deptriage/deptriage/internal/config"
	"github.com/spf13/cobra"
)

const providerProbeTimeout = 2 * time.Second

type providerDiagnostic struct {
	name   string
	status string
	model  string
	detail string
}

type providerProbe func(context.Context, string, string, string, time.Duration) error

func providersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "providers",
		Short: "Show configured provider status and reachability",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := config.LoadDefault()
			if err != nil {
				return fmt.Errorf("load provider configuration: %w", err)
			}
			rows := inspectProviders(cmd.Context(), cfg, probeEndpoint)
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(writer, "PROVIDER\tSTATUS\tMODEL\tDETAIL"); err != nil {
				return fmt.Errorf("write provider table header: %w", err)
			}
			for _, row := range rows {
				if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", row.name, row.status, row.model, row.detail); err != nil {
					return fmt.Errorf("write provider table row: %w", err)
				}
			}
			if err := writer.Flush(); err != nil {
				return fmt.Errorf("flush provider table: %w", err)
			}
			return nil
		},
	}
}

func inspectProviders(ctx context.Context, cfg config.Config, probe providerProbe) []providerDiagnostic {
	rows := make([]providerDiagnostic, 0, 5)
	rows = append(rows, providerDiagnostic{name: "fake", status: "configured", model: "fake", detail: "deterministic test provider"})

	ollamaHost := ollamaHost(cfg)
	ollamaRow := providerDiagnostic{name: "ollama", model: cfg.Ollama.Model}
	if ollamaRow.model == "" {
		ollamaRow.model = "llama3.2:1b"
	}
	if err := probe(ctx, ollamaHost+"/api/tags", "", "", providerProbeTimeout); err != nil {
		ollamaRow.status, ollamaRow.detail = "unreachable", err.Error()
	} else {
		ollamaRow.status, ollamaRow.detail = "reachable", ollamaHost
	}
	rows = append(rows, ollamaRow)

	for _, provider := range []string{"gemini", "groq", "openai"} {
		key := providerKey(provider, cfg)
		model, endpoint := providerDetails(provider, cfg)
		row := providerDiagnostic{name: provider, model: model}
		if key == "" {
			row.status, row.detail = "missing key", "set provider environment key or config [keys]."+provider
		} else if err := probe(ctx, endpoint, provider, key, providerProbeTimeout); err != nil {
			row.status, row.detail = "unreachable", err.Error()
		} else {
			row.status, row.detail = "reachable", "API responded successfully"
		}
		rows = append(rows, row)
	}
	return rows
}

func providerKey(provider string, cfg config.Config) string {
	envName := map[string]string{"gemini": "GEMINI_API_KEY", "groq": "GROQ_API_KEY", "openai": "OPENAI_API_KEY"}[provider]
	return resolveAPIKeyFromEnvironment(envName, cfg.Keys[provider])
}

func resolveAPIKeyFromEnvironment(envName, configured string) string {
	return resolveAPIKey(os.Getenv(envName), configured)
}

func providerDetails(provider string, cfg config.Config) (model, endpoint string) {
	switch provider {
	case "gemini":
		model = cfg.Gemini.Model
		endpoint = "https://generativelanguage.googleapis.com/v1beta/models"
	case "groq":
		model = cfg.Groq.Model
		endpoint = "https://api.groq.com/openai/v1/models"
	case "openai":
		model = cfg.OpenAI.Model
		endpoint = strings.TrimRight(cfg.OpenAI.BaseURL, "/") + "/models"
	}
	return model, endpoint
}

func ollamaHost(cfg config.Config) string {
	if envHost := os.Getenv("OLLAMA_HOST"); envHost != "" {
		return strings.TrimRight(envHost, "/")
	}
	if strings.TrimSpace(cfg.Ollama.Host) != "" {
		return strings.TrimRight(cfg.Ollama.Host, "/")
	}
	return "http://localhost:11434"
}

func probeEndpoint(ctx context.Context, endpoint, provider, key string, timeout time.Duration) error {
	probeCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		probeCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build reachability probe: %w", err)
	}
	if provider == "gemini" {
		request.Header.Set("x-goog-api-key", key)
	} else if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("GET %s returned HTTP %d", endpoint, response.StatusCode)
	}
	return nil
}

func providerKeyPresence(cfg config.Config) []string {
	var configured []string
	for _, provider := range []string{"gemini", "groq", "openai"} {
		if providerKey(provider, cfg) != "" {
			configured = append(configured, provider)
		}
	}
	return configured
}

func resolveProviderName(flagValue string, flagChanged bool, environmentValue, configuredValue string, cloudProviders []string, ollamaReachable bool) (string, string) {
	if flagChanged {
		return strings.ToLower(strings.TrimSpace(flagValue)), "--provider flag was explicitly set"
	}
	if strings.TrimSpace(environmentValue) != "" {
		return strings.ToLower(strings.TrimSpace(environmentValue)), "selected from DEPTRIAGE_PROVIDER"
	}
	if strings.TrimSpace(configuredValue) != "" {
		return strings.ToLower(strings.TrimSpace(configuredValue)), "selected from default_provider in config"
	}
	if len(cloudProviders) > 0 {
		return cloudProviders[0], "auto-selected configured cloud key (priority: gemini, groq, openai)"
	}
	if ollamaReachable {
		return "ollama", "auto-selected reachable local Ollama"
	}
	return "", ""
}

func onboardingMessage(reasons []string) string {
	return `No LLM provider configured. Pick one:

  1) Local, free, no key — install Ollama and pull a small model:
       brew install ollama && ollama serve
       ollama pull llama3.2:1b
     then: deptriage check -r owner/repo --provider ollama

  2) Free cloud key (30 seconds) — Google AI Studio:
       https://aistudio.google.com/apikey
       deptriage config set-key gemini <key>
     then: deptriage check -r owner/repo

Why did we show this?
  - ` + strings.Join(reasons, "\n  - ")
}

func autoDetection(cfg config.Config, probe providerProbe, ctx context.Context) ([]string, bool) {
	cloud := providerKeyPresence(cfg)
	ollamaReachable := probe(ctx, ollamaHost(cfg)+"/api/tags", "", "", providerProbeTimeout) == nil
	return cloud, ollamaReachable
}

func onboardingReasons(cfg config.Config, probe providerProbe, ctx context.Context) []string {
	cloud, ollamaReachable := autoDetection(cfg, probe, ctx)
	reasons := []string{"--provider was not set", "DEPTRIAGE_PROVIDER is not set", "default_provider is not set in config"}
	if len(cloud) == 0 {
		reasons = append(reasons, "no Gemini, Groq, or OpenAI API key is configured")
	}
	if !ollamaReachable {
		reasons = append(reasons, "Ollama is unreachable at "+ollamaHost(cfg))
	}
	return reasons
}
