package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/deptriage/deptriage/internal/config"
	depgh "github.com/deptriage/deptriage/internal/github"
	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/llm/fake"
	"github.com/deptriage/deptriage/internal/llm/gemini"
	"github.com/deptriage/deptriage/internal/llm/groq"
	"github.com/deptriage/deptriage/internal/llm/ollama"
	"github.com/deptriage/deptriage/internal/llm/openai"
	"github.com/deptriage/deptriage/internal/report"
	"github.com/deptriage/deptriage/internal/triage"
	"github.com/spf13/cobra"
)

// ExitError carries a process status for expected check outcomes.
type ExitError struct {
	Code int
	Err  error
}

// Error returns the wrapped exit condition.
func (e *ExitError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying error.
func (e *ExitError) Unwrap() error { return e.Err }

type checkDependencies struct {
	github   depgh.Client
	provider llm.Provider
}

// newCheckCommand creates the production check command.
func newCheckCommand() *cobra.Command {
	return makeCheckCommand(nil)
}

func makeCheckCommand(deps *checkDependencies) *cobra.Command {
	var repo, providerName, model, failOn, token, fakeVerdict string
	var jsonOutput, includeAll, dryRun bool
	var limit int
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Triage open dependency update pull requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			owner, repository, ok := strings.Cut(repo, "/")
			if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(repository) == "" || strings.Contains(repository, "/") {
				return &ExitError{Code: 2, Err: fmt.Errorf("--repo must be in owner/repo format")}
			}
			if failOn != "risk" && failOn != "review" && failOn != "none" {
				return &ExitError{Code: 2, Err: fmt.Errorf("--fail-on must be risk, review, or none")}
			}
			if providerName != "" && providerName != "fake" && providerName != "ollama" && providerName != "gemini" && providerName != "groq" && providerName != "openai" {
				return &ExitError{Code: 2, Err: fmt.Errorf("provider %q not implemented in M4", providerName)}
			}
			if limit < 1 {
				return &ExitError{Code: 2, Err: fmt.Errorf("--limit must be greater than zero")}
			}
			if timeout < 0 {
				return &ExitError{Code: 2, Err: fmt.Errorf("--timeout must not be negative")}
			}
			if deps == nil {
				cfg, _, err := config.LoadDefault()
				if err != nil {
					return &ExitError{Code: 2, Err: fmt.Errorf("load provider configuration: %w", err)}
				}
				flagSet := cmd.Flags().Changed("provider")
				cloud, ollamaReachable := []string(nil), false
				if !flagSet && strings.TrimSpace(os.Getenv("DEPTRIAGE_PROVIDER")) == "" && strings.TrimSpace(cfg.DefaultProvider) == "" {
					cloud, ollamaReachable = autoDetection(cfg, probeEndpoint, cmd.Context())
				}
				providerName, _ = resolveProviderName(providerName, flagSet, os.Getenv("DEPTRIAGE_PROVIDER"), cfg.DefaultProvider, cloud, ollamaReachable)
				if providerName == "" {
					return &ExitError{Code: 2, Err: errors.New(onboardingMessage(onboardingReasons(cfg, probeEndpoint, cmd.Context())))}
				}
				providerName = strings.ToLower(strings.TrimSpace(providerName))
				if providerName != "fake" && providerName != "ollama" && providerName != "gemini" && providerName != "groq" && providerName != "openai" {
					return &ExitError{Code: 2, Err: fmt.Errorf("provider %q not implemented in M4", providerName)}
				}
				var selectedProvider llm.Provider
				switch providerName {
				case "fake":
					selectedProvider, err = fake.New(fakeVerdict)
					model = config.Resolve(model, "", "fake")
				case "ollama":
					client := ollama.New(cfg.Ollama.Host, cfg.Ollama.Model)
					selectedProvider, model = client, client.Model(config.Resolve(model, "", cfg.DefaultModel))
				case "gemini":
					key := providerKey(providerName, cfg)
					if key == "" {
						return &ExitError{Code: 2, Err: fmt.Errorf("provider gemini requires GEMINI_API_KEY or config [keys].gemini — get a free key at https://aistudio.google.com/apikey")}
					}
					client := gemini.New(key, cfg.Gemini.Model)
					selectedProvider, model = client, client.Model(model)
				case "groq":
					key := providerKey(providerName, cfg)
					if key == "" {
						return &ExitError{Code: 2, Err: fmt.Errorf("provider groq requires GROQ_API_KEY or config [keys].groq — get a free key at https://console.groq.com/keys")}
					}
					client := groq.New(key, cfg.Groq.Model)
					selectedProvider, model = client, client.Model(model)
				case "openai":
					key := providerKey(providerName, cfg)
					if key == "" {
						return &ExitError{Code: 2, Err: fmt.Errorf("provider openai requires OPENAI_API_KEY or config [keys].openai — get a free key at https://platform.openai.com/api-keys")}
					}
					client := openai.New(key, cfg.OpenAI.BaseURL, cfg.OpenAI.Model)
					selectedProvider, model = client, client.Model(model)
				}
				if err != nil {
					return &ExitError{Code: 2, Err: err}
				}
				if providerName == "fake" && model == "" {
					model = "fake"
				}
				if token == "" {
					token = os.Getenv("GITHUB_TOKEN")
				}
				if token == "" {
					var err error
					token, err = githubTokenFromDotEnv(".env")
					if err != nil {
						return fmt.Errorf("load GitHub token from .env: %w", err)
					}
				}
				if token == "" {
					return &ExitError{Code: 2, Err: fmt.Errorf("GITHUB_TOKEN not found in --github-token, environment, or .env; create a classic PAT with public_repo (or repo) read scope")}
				}
				deps = &checkDependencies{github: depgh.NewClientWithTimeout(cmd.Context(), token, timeout), provider: selectedProvider}
			}
			ctx := cmd.Context()
			prs, err := deps.github.ListOpenDependencyPRs(ctx, owner, repository, depgh.ListOptions{Limit: limit, IncludeAllAuthors: includeAll})
			if err != nil {
				return fmt.Errorf("list pull requests: %w", err)
			}
			results := make([]report.Result, 0, len(prs))
			partial := false
			for _, pr := range prs {
				prContext, fetchErr := deps.github.FetchContext(ctx, owner, repository, pr.Number)
				if fetchErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: PR #%d: fetch context: %v\n", pr.Number, fetchErr)
					partial = true
					continue
				}
				signals := triage.EvaluateSignals(pr, prContext)
				request := llm.Request{
					PR: pr, Context: prContext, Model: model, Temp: 0.0, Timeout: timeout,
					Schema: llm.VerdictSchema,
					System: "Classify dependency update pull requests as safe (low risk), review (human attention needed), or risk (high risk, breaking, or security relevant). Lockfile-only changes are usually safe; major version bumps and security advisories are usually risk. Return only JSON matching the supplied schema.",
					User:   buildClassificationPrompt(pr, prContext, signals),
				}
				if dryRun {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "PR #%d SYSTEM PROMPT:\n%s\n\nUSER PROMPT:\n%s\n\n", pr.Number, request.System, request.User); err != nil {
						return fmt.Errorf("write dry-run prompts: %w", err)
					}
					continue
				}
				verdict, classifyErr := deps.provider.Classify(llm.WithRequestTimeout(ctx, request.Timeout), request)
				if classifyErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: PR #%d: classify: %v\n", pr.Number, classifyErr)
					partial = true
					continue
				}
				degraded := verdict.Degraded
				results = append(results, report.Result{PR: pr, Context: prContext, Signals: signals, Verdict: triage.Merge(verdict, signals), Degraded: degraded})
			}
			if dryRun {
				return nil
			}
			if jsonOutput {
				err = report.RenderJSON(cmd.OutOrStdout(), results, owner+"/"+repository, deps.provider.Name(), model)
			} else {
				err = report.RenderHuman(cmd.OutOrStdout(), results, deps.provider.Name(), model, true)
			}
			if err != nil {
				return err
			}
			if partial {
				return &ExitError{Code: 4, Err: errors.New("some pull requests could not be classified")}
			}
			if failOn == "risk" {
				for _, result := range results {
					if result.Verdict.Verdict == "risk" {
						return &ExitError{Code: 3, Err: errors.New("risk verdict threshold exceeded")}
					}
				}
			} else if failOn == "review" {
				for _, result := range results {
					if result.Verdict.Verdict == "risk" || result.Verdict.Verdict == "review" {
						return &ExitError{Code: 3, Err: errors.New("review verdict threshold exceeded")}
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&repo, "repo", "r", "", "GitHub repository in owner/repo format")
	cmd.Flags().StringVar(&providerName, "provider", "", "LLM provider (auto, fake, ollama, gemini, groq, or openai)")
	cmd.Flags().StringVar(&model, "model", "", "model name (provider default when omitted)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().IntVar(&limit, "limit", 25, "maximum open pull requests to process")
	cmd.Flags().BoolVar(&includeAll, "include-all-authors", false, "include PRs from all authors")
	cmd.Flags().StringVar(&failOn, "fail-on", "none", "exit 3 when risk, review, or none is reached")
	cmd.Flags().DurationVar(&timeout, "timeout", 90*time.Second, "per-request timeout")
	cmd.Flags().StringVar(&token, "github-token", "", "GitHub personal access token (or GITHUB_TOKEN)")
	cmd.Flags().StringVar(&fakeVerdict, "fake-verdict", "", "internal test override for the fake provider")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print prompts without calling an LLM")
	_ = cmd.Flags().MarkHidden("fake-verdict")

	return cmd
}

func resolveAPIKey(environment, configured string) string {
	if strings.TrimSpace(environment) != "" {
		return strings.TrimSpace(environment)
	}
	return strings.TrimSpace(configured)
}

func buildClassificationPrompt(pr depgh.PR, context depgh.PRContext, signals []triage.Signal) string {
	signalNames := make([]string, 0, len(signals))
	for _, signal := range signals {
		signalNames = append(signalNames, fmt.Sprintf("%s (%+d): %s", signal.Name, signal.Score, signal.Reason))
	}
	return fmt.Sprintf("Pull request: #%d %s\nAuthor: %s\nBody:\n%s\nDependency: %s\nUpdate: %s -> %s\nChanged files: %d\nNon-lockfile additions/deletions: %d/%d\nCI: %s\nDeterministic signals: %s\n\nReturn a verdict, confidence, one-sentence summary, reasons, and the two boolean risk flags.", pr.Number, pr.Title, pr.Author, pr.Body, context.DependencyName, context.FromVersion, context.ToVersion, len(context.ChangedFiles), context.Additions, context.Deletions, context.CIStatus, strings.Join(signalNames, "; "))
}
