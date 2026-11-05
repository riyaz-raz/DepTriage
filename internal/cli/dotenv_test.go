package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitHubTokenFromDotEnv(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantFound bool
	}{
		{"plain assignment", "GITHUB_TOKEN=sample-token\n", true},
		{"quoted assignment", "export GITHUB_TOKEN=\"sample-token\"\n", true},
		{"unrelated variable", "OTHER_TOKEN=sample-token\n", false},
		{"comment", "# GITHUB_TOKEN=sample-token\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := githubTokenFromDotEnv(path)
			if err != nil {
				t.Fatalf("read dotenv: %v", err)
			}
			if (got != "") != tc.wantFound {
				t.Fatalf("token found = %t, want %t", got != "", tc.wantFound)
			}
		})
	}
}
