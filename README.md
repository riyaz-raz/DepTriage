# deptriage

`deptriage` triages open Dependabot and Renovate pull requests with deterministic risk signals and an optional LLM, without ever writing to GitHub.

## 30-second quickstart

### Local and free with Ollama

```sh
curl -fsSL https://raw.githubusercontent.com/deptriage/deptriage/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
export GITHUB_TOKEN=ghp_your_read_only_token

ollama serve
ollama pull llama3.2:1b
deptriage check -r owner/repo
```

Ollama mode makes no cloud LLM calls. A GitHub token is still required for read-only repository access.

### Gemini

```sh
curl -fsSL https://raw.githubusercontent.com/deptriage/deptriage/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
export GITHUB_TOKEN=ghp_your_read_only_token

deptriage config set-key gemini YOUR_GEMINI_KEY
deptriage check -r owner/repo
```

You can also pass a provider explicitly:

```sh
deptriage check -r owner/repo --provider gemini --model gemini-3.8-flash
```

## Example output

```text
PR   DEPENDENCY                         UPDATE       CI       VERDICT  CONFIDENCE  SIGNALS
#764 dawidd6/action-download-artifact  27 -> 28     passing  safe     90%         patch_version_bump,ci_passing
#763 github-actions group               (group)      passing  review   82%         minor_version_bump,lockfile_only
2 PRs: 1 safe, 1 review, 0 risk (provider: ollama/llama3.2:1b)
```

## Provider selection

When `--provider` is omitted, selection follows this order:

1. `--provider`
2. `DEPTRIAGE_PROVIDER`
3. `default_provider` in `$XDG_CONFIG_HOME/deptriage/config.toml` or `~/.config/deptriage/config.toml`
4. Auto-detection: configured cloud keys in Gemini, Groq, OpenAI order, then reachable Ollama

Use `deptriage providers` to inspect configuration and reachability. API keys are never printed. Use `deptriage check --dry-run -r owner/repo` to fetch GitHub context and print the prompts without making an LLM call.

Supported providers are `ollama`, `gemini`, `groq`, `openai`, and the deterministic `fake` provider for tests. Provider keys resolve from the provider environment variable before the config `[keys]` table.

## Verdicts and signals

- **safe** — low risk and likely suitable for automated merging after normal CI policy.
- **review** — a human should inspect the change.
- **risk** — breaking, security-relevant, or otherwise high risk.

The rule layer is deterministic and explainable. It detects major/minor/patch bumps, CI state, security labels or advisories, development dependencies, lockfile-only changes, and large diffs. Signals are shown in every report. A strong positive signal can force `risk`; a positive aggregate score downgrades an LLM `safe` result to `review`; an LLM `risk` result is never downgraded. Safe results with confidence below 0.7 are also downgraded to `review`.

## Configuration

```sh
deptriage config init
deptriage config show
deptriage config set-key gemini <key>
```

The config file is TOML and is stored at `$XDG_CONFIG_HOME/deptriage/config.toml`, falling back to `~/.config/deptriage/config.toml`. GitHub credentials can be supplied with `GITHUB_TOKEN`, `--github-token`, or a local `.env` containing `GITHUB_TOKEN`. A classic GitHub PAT with `public_repo` (or `repo` for private repositories) read scope is sufficient.

## JSON output

Use `--json` for a stable machine-readable document:

```json
{
  "schema_version": 1,
  "repo": "owner/repo",
  "provider": "gemini",
  "model": "gemini-3.8-flash",
  "generated_at": "2026-01-01T12:00:00Z",
  "summary": {"total": 1, "safe": 1, "review": 0, "risk": 0, "degraded": 0},
  "items": [
    {
      "number": 764,
      "title": "build(deps): bump example from 1 to 2",
      "author": "dependabot[bot]",
      "url": "https://github.com/owner/repo/pull/764",
      "dependency": "example",
      "from": "1",
      "to": "2",
      "ci": "passing",
      "verdict": "safe",
      "confidence": 0.9,
      "summary": "Patch update with passing checks.",
      "reasons": ["small dependency update"],
      "signals": ["patch_version_bump", "ci_passing"],
      "degraded": false
    }
  ]
}
```

## Exit codes

| Code | Meaning |
| ---: | --- |
| 0 | Successful run; no verdict exceeded `--fail-on`. |
| 1 | Unexpected error. |
| 2 | Usage or configuration error, including missing GitHub/provider credentials. |
| 3 | The `--fail-on` threshold was exceeded. |
| 4 | Partial failure; the report was printed but one or more PRs could not be classified. |

For CI, use `--fail-on review` to fail on both `review` and `risk`, or `--fail-on risk` to fail only on `risk`.

## Security and privacy

- GitHub access is read-only: deptriage lists PRs and reads metadata, files, statuses, and check runs. It never comments, labels, approves, or merges.
- There is no telemetry, analytics, or phone-home behavior.
- API keys are read from environment variables or the private TOML config and are redacted by `config show`.
- Ollama mode keeps LLM prompts and responses local to the Ollama server. GitHub API traffic still goes to GitHub.
- Do not commit `.env` or configuration files containing credentials.

## Development

```sh
go test ./...
go build ./...
go vet ./...
make build VERSION=dev
```

Release artifacts are built by GoReleaser for Linux, macOS, and Windows on amd64 and arm64. Tags matching `v*` invoke `.github/workflows/release.yml`.

## Roadmap

- Future hosted GitHub App integration for webhook-driven triage (not implemented).
- Optional Homebrew tap publishing once the project tap repository is established.
