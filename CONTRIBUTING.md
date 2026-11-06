# Contributing to deptriage

Thanks for your interest in contributing! Issues, bug reports, documentation improvements, and pull requests are welcome.

## Before you start

- Check existing issues and pull requests to see whether the work is already being discussed.
- For a significant change, open an issue first so we can agree on the approach.
- Keep changes focused and consistent with the existing Go code and project behavior.

## Set up and verify

Use a supported Go toolchain for the version declared in `go.mod`. From the repository root, run:

```sh
go test ./...
go build ./...
go vet ./...
```

If you change release or build behavior, you can also verify the Make target:

```sh
make build VERSION=dev
```

Please add or update tests when changing behavior. Keep tests deterministic and avoid requiring live GitHub or LLM provider access; use the existing test patterns and fake provider where appropriate.

## Pull requests

A helpful pull request includes:

- A clear description of the problem and the change.
- Tests or a brief explanation of why tests are not applicable.
- Documentation updates for user-facing behavior or configuration changes.
- Any relevant compatibility, security, or operational considerations.

Keep each pull request focused. Do not include unrelated formatting or refactoring changes.

## Security and credentials

Never commit API keys, GitHub tokens, `.env` files, or configuration files containing secrets. Use test doubles rather than real credentials in tests. If you discover a security vulnerability, do not disclose it in a public issue; contact the maintainers privately through the project's GitHub security reporting channel.

## Project scope

`deptriage` is designed to access GitHub read-only. Contributions must not add behavior that comments on, labels, approves, merges, or otherwise writes to repositories without an explicit, separately reviewed change to the project's security model.
