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
	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/llm/fake"
	"github.com/deptriage/deptriage/internal/triage"
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

type fixedGitHubClient struct{ pr depgh.PR }

func (c fixedGitHubClient) ListOpenDependencyPRs(context.Context, string, string, depgh.ListOptions) ([]depgh.PR, error) {
	return []depgh.PR{c.pr}, nil
}

func (c fixedGitHubClient) FetchContext(context.Context, string, string, int) (depgh.PRContext, error) {
	return depgh.PRContext{PR: c.pr, CIStatus: depgh.CIUnknown}, nil
}

type failingProvider struct{ err error }

func (p failingProvider) Name() string { return "gemini" }
func (p failingProvider) Classify(context.Context, llm.Request) (triage.Verdict, error) {
	return triage.Verdict{}, p.err
}

func TestClassifyErrorIsReportedAsPartialExitFour(t *testing.T) {
	pr := depgh.PR{Number: 9, Title: "Update dependency", Author: "dependabot[bot]"}
	errText := `gemini: model "gemini-old" not available. Server suggests: "gemini-new". Retry with: --model gemini-new (or update config [gemini].model).`
	cmd := makeCheckCommand(&checkDependencies{github: fixedGitHubClient{pr: pr}, provider: failingProvider{err: errors.New(errText)}})
	var output, stderr strings.Builder
	cmd.SetOut(&output)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"-r", "owner/repo", "--provider", "gemini"})
	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 4 {
		t.Fatalf("exit error = %v, want code 4", err)
	}
	if !strings.Contains(stderr.String(), "--model gemini-new") || !strings.Contains(output.String(), "0 PRs:") {
		t.Fatalf("missing actionable partial report; output=%q stderr=%q", output.String(), stderr.String())
	}
}

func TestBuildClassificationPromptTruncatesBodyAndBudget(t *testing.T) {
	pr := depgh.PR{Number: 1, Title: "Update dependency", Author: "dependabot[bot]", Body: strings.Repeat("body ", 2000)}
	prContext := depgh.PRContext{CIStatus: depgh.CIPassing, DependencyName: "example", FromVersion: "1", ToVersion: "2"}
	prompt := buildClassificationPromptWithBudget(pr, prContext, nil, 500)
	if len([]rune(prompt)) > 500 {
		t.Fatalf("prompt length = %d, want <= 500", len([]rune(prompt)))
	}
	if !strings.Contains(prompt, "Prompt truncated") {
		t.Fatalf("truncation note missing from prompt: %q", prompt)
	}
}

func TestBuildClassificationPromptTruncatesBodyAtFourThousandCharacters(t *testing.T) {
	pr := depgh.PR{Body: strings.Repeat("x", 5000)}
	prompt := buildClassificationPromptWithBudget(pr, depgh.PRContext{}, nil, 12000)
	if strings.Count(prompt, "x") != 4000 {
		t.Fatalf("body length in prompt = %d, want 4000", strings.Count(prompt, "x"))
	}
}

func TestCheckUsageExitCodes(t *testing.T) {
	cmd := makeCheckCommand(&checkDependencies{})
	cmd.SetArgs([]string{"-r", "o/r", "--provider", "bogus"})
	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "not implemented in M4") {
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

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct{ provider, keyEnv, message string }{
		{"gemini", "GEMINI_API_KEY", "provider gemini requires GEMINI_API_KEY or config [keys].gemini"},
		{"groq", "GROQ_API_KEY", "provider groq requires GROQ_API_KEY or config [keys].groq"},
		{"openai", "OPENAI_API_KEY", "provider openai requires OPENAI_API_KEY or config [keys].openai"},
	} {
		t.Run("missing "+tc.provider+" key", func(t *testing.T) {
			t.Setenv(tc.keyEnv, "")
			cmd := makeCheckCommand(nil)
			cmd.SetArgs([]string{"-r", "o/r", "--provider", tc.provider})
			err := cmd.Execute()
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("missing key error = %v", err)
			}
		})
	}
}
