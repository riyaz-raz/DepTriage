package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	cases := []struct {
		name, flag, env, configured, want string
	}{
		{"flag wins", "openai", "groq", "gemini", "openai"},
		{"environment wins", "", "groq", "gemini", "groq"},
		{"config fallback", "", "", "gemini", "gemini"},
		{"empty", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.flag, tc.env, tc.configured); got != tc.want {
				t.Fatalf("Resolve() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadSaveAndRedact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Defaults()
	cfg.Keys["gemini"] = "abcdefghijkl"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if got := loaded.Redacted().Keys["gemini"]; got != "****ijkl" {
		t.Fatalf("redacted key = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}
}
