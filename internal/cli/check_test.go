package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	depgh "github.com/deptriage/deptriage/internal/github"
	"github.com/deptriage/deptriage/internal/llm/fake"
)

var updateCheckGolden = flag.Bool("update", false, "update check command golden files")

func TestCheckEndToEndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		labels     string
		failOn     string
		wantCode   int
		jsonOutput bool
		golden     string
	}{
		{"safe exits zero", `[]`, "risk", 0, true, "check-json.golden"},
		{"risk exits three", `[{"name":"security"}]`, "risk", 3, false, "check-human.golden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls"):
					_, _ = w.Write([]byte(`[ {"number":1,"title":"Bump widget from 1.0.0 to 1.0.1","body":"","user":{"login":"dependabot[bot]"},"labels":` + tc.labels + `,"head":{"sha":""},"base":{"ref":"main"},"html_url":"https://github.com/o/r/pull/1"} ]`))
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/1"):
					_, _ = w.Write([]byte(`{"number":1,"title":"Bump widget from 1.0.0 to 1.0.1","body":"","user":{"login":"dependabot[bot]"},"labels":` + tc.labels + `,"head":{"sha":""},"base":{"ref":"main"},"html_url":"https://github.com/o/r/pull/1"}`))
				case strings.HasSuffix(r.URL.Path, "/pulls/1/files"):
					_, _ = w.Write([]byte(`[]`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			githubClient, err := depgh.NewClientWithBaseURL(context.Background(), "", server.URL+"/")
			if err != nil {
				t.Fatal(err)
			}
			provider, err := fake.New("")
			if err != nil {
				t.Fatal(err)
			}
			deps := &checkDependencies{github: githubClient, provider: provider}
			command := makeCheckCommand(deps)
			var output, stderr strings.Builder
			command.SetOut(&output)
			command.SetErr(&stderr)
			args := []string{"-r", "o/r", "--provider", "fake", "--fail-on", tc.failOn, "--timeout", "5s"}
			if tc.jsonOutput {
				args = append(args, "--json")
			}
			command.SetArgs(args)
			err = command.Execute()
			code := 0
			if err != nil {
				var exitErr *ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("unexpected error: %v; stderr: %s", err, stderr.String())
				}
				code = exitErr.Code
			}
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d; output=%s stderr=%s", code, tc.wantCode, output.String(), stderr.String())
			}
			actual := []byte(output.String())
			if tc.jsonOutput {
				var document map[string]any
				if err := json.Unmarshal(actual, &document); err != nil {
					t.Fatalf("invalid JSON output: %v: %s", err, output.String())
				}
				if document["schema_version"] != float64(1) {
					t.Fatalf("unexpected JSON schema version: %#v", document["schema_version"])
				}
				actual = regexp.MustCompile(`"generated_at": "[^"]+"`).ReplaceAll(actual, []byte(`"generated_at": "<timestamp>"`))
			} else if !strings.Contains(output.String(), "DEPENDENCY") {
				t.Fatalf("missing report output: %s", output.String())
			}
			goldenPath := filepath.Join("..", "..", "testdata", tc.golden)
			if *updateCheckGolden {
				if err := os.WriteFile(goldenPath, actual, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(actual) != string(want) {
				t.Fatalf("check golden mismatch for %s; run go test ./internal/cli -update", tc.golden)
			}

		})
	}
}

func TestCheckUsageExitCodes(t *testing.T) {
	cmd := makeCheckCommand(&checkDependencies{})
	cmd.SetArgs([]string{"-r", "o/r", "--provider", "ollama"})
	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "not implemented in M2") {
		t.Fatalf("provider error = %v", err)
	}

	t.Setenv("GITHUB_TOKEN", "")
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(workingDirectory) }()
	cmd = makeCheckCommand(nil)
	cmd.SetArgs([]string{"-r", "o/r", "--provider", "fake"})
	err = cmd.Execute()
	if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "classic PAT with public_repo") {
		t.Fatalf("missing token error = %v", err)
	}
}
