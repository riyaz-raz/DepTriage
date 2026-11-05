package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/deptriage/deptriage/internal/config"
	depgh "github.com/deptriage/deptriage/internal/github"
	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/llm/fake"
	"github.com/deptriage/deptriage/internal/llm/ollama"
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
	var jsonOutput, includeAll bool
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
			if providerName == "" {
				providerName = "fake"
			}
			if providerName != "fake" && providerName != "ollama" {
				return &ExitError{Code: 2, Err: fmt.Errorf("provider %q not implemented in M3", providerName)}
			}
			if providerName == "fake" && model == "" {
				model = "fake"
			}
			if limit < 1 {
				return &ExitError{Code: 2, Err: fmt.Errorf("--limit must be greater than zero")}
			}
			if timeout <= 0 {
				return &ExitError{Code: 2, Err: fmt.Errorf("--timeout must be greater than zero")}
			}
			if deps == nil {
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
				ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
				defer cancel()
				deps = &checkDependencies{github: depgh.NewClient(ctx, token)}
				if providerName == "fake" {
					var err error
					deps.provider, err = fake.New(fakeVerdict)
					if err != nil {
						return &ExitError{Code: 2, Err: err}
					}
					if model == "" {
						model = "fake"
					}
				} else {
					cfg, _, err := config.LoadDefault()
					if err != nil {
						return fmt.Errorf("load Ollama configuration: %w", err)
					}
					ollamaClient := ollama.New(cfg.Ollama.Host, cfg.Ollama.Model)
					if model == "" {
						model = ollamaClient.Model("")
					}
					deps.provider = ollamaClient
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
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
				request := llm.Request{PR: pr, Context: prContext, Model: model, Temp: 0.2, Schema: llm.VerdictSchema}
				if deps.provider.Name() == "ollama" {
					request.System = "Classify dependency update pull requests as safe (low risk), review (human attention needed), or risk (high risk, breaking, or security relevant). Lockfile-only changes are usually safe; major version bumps and security advisories are usually risk. Return only JSON matching the supplied schema."
					request.User = buildClassificationPrompt(pr, prContext, signals)
				}
				verdict, classifyErr := deps.provider.Classify(ctx, request)
				if classifyErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: PR #%d: classify: %v\n", pr.Number, classifyErr)
					partial = true
					continue
				}
				degraded := verdict.Degraded
				results = append(results, report.Result{PR: pr, Context: prContext, Signals: signals, Verdict: triage.Merge(verdict, signals), Degraded: degraded})
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
	cmd.Flags().StringVar(&providerName, "provider", "fake", "LLM provider (fake or ollama)")
	cmd.Flags().StringVar(&model, "model", "", "model name (provider default when omitted)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().IntVar(&limit, "limit", 25, "maximum open pull requests to process")
	cmd.Flags().BoolVar(&includeAll, "include-all-authors", false, "include PRs from all authors")
	cmd.Flags().StringVar(&failOn, "fail-on", "none", "exit 3 when risk, review, or none is reached")
	cmd.Flags().DurationVar(&timeout, "timeout", 90*time.Second, "overall check timeout")
	cmd.Flags().StringVar(&token, "github-token", "", "GitHub personal access token (or GITHUB_TOKEN)")
	cmd.Flags().StringVar(&fakeVerdict, "fake-verdict", "", "internal test override for the fake provider")
	_ = cmd.Flags().MarkHidden("fake-verdict")

	return cmd
}

func buildClassificationPrompt(pr depgh.PR, context depgh.PRContext, signals []triage.Signal) string {
	signalNames := make([]string, 0, len(signals))
	for _, signal := range signals {
		signalNames = append(signalNames, fmt.Sprintf("%s (%+d): %s", signal.Name, signal.Score, signal.Reason))
	}
	return fmt.Sprintf("Pull request: #%d %s\nAuthor: %s\nBody:\n%s\nDependency: %s\nUpdate: %s -> %s\nChanged files: %d\nNon-lockfile additions/deletions: %d/%d\nCI: %s\nDeterministic signals: %s\n\nReturn a verdict, confidence, one-sentence summary, reasons, and the two boolean risk flags.", pr.Number, pr.Title, pr.Author, pr.Body, context.DependencyName, context.FromVersion, context.ToVersion, len(context.ChangedFiles), context.Additions, context.Deletions, context.CIStatus, strings.Join(signalNames, "; "))
}
