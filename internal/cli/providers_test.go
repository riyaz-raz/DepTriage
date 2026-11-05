package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/deptriage/deptriage/internal/config"
)

func TestResolveProviderNamePrecedence(t *testing.T) {
	cases := []struct {
		name       string
		flag       string
		flagSet    bool
		env        string
		configured string
		cloud      []string
		ollama     bool
		want       string
	}{
		{"flag", "groq", true, "gemini", "openai", []string{"gemini"}, true, "groq"},
		{"environment", "", false, "openai", "gemini", []string{"groq"}, true, "openai"},
		{"config", "", false, "", "ollama", []string{"gemini"}, true, "ollama"},
		{"cloud auto", "", false, "", "", []string{"openai"}, true, "openai"},
		{"ollama auto", "", false, "", "", nil, true, "ollama"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := resolveProviderName(tc.flag, tc.flagSet, tc.env, tc.configured, tc.cloud, tc.ollama)
			if got != tc.want {
				t.Fatalf("provider = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOnboardingMessageIncludesExactChoicesAndReasons(t *testing.T) {
	cfg := config.Defaults()
	probe := func(context.Context, string, string, string, time.Duration) error {
		return context.DeadlineExceeded
	}
	message := onboardingMessage(onboardingReasons(cfg, probe, context.Background()))
	for _, want := range []string{
		"No LLM provider configured. Pick one:",
		"brew install ollama && ollama serve",
		"https://aistudio.google.com/apikey",
		"Why did we show this?",
		"Ollama is unreachable",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("onboarding message missing %q: %s", want, message)
		}
	}
}
