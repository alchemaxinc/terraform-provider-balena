# Contributing to Terraform Provider for Balena Cloud

Thank you for your interest in contributing! This document covers the process for contributing to this project.

## Requirements

- [Go](https://golang.org/doc/install)
- [Terraform](https://developer.hashicorp.com/terraform/downloads)
- [Node.js](https://nodejs.org/) (used to run `prettier` via `npx` during lint/format/docs)
- [golangci-lint](https://golangci-lint.run/welcome/install-local/)
- [prettier](https://prettier.io/docs/en/install.html) (for formatting documentation)

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone git@github.com:YOUR_USER/terraform-provider-balena.git`
3. Create a branch: `git checkout -b feat/my-feature`
4. Make your changes
5. Run checks: `make lint && make test`
6. Commit using [Conventional Commits](https://www.conventionalcommits.org/): `git commit -m "feat: add widget resource"`
7. Push and open a pull request against `main`

## Development

```shell
make build          # Build the provider
make install        # Install locally
make test           # Unit tests
make testacc        # Acceptance tests (requires BALENA_API_TOKEN)
make lint           # Run linters
make docs           # Generate documentation
```

## Release Process

This repository follows semantic versioning with an automated release pipeline:

1. Development work happens on feature branches off `main`.
2. PRs are opened against `main`.
3. On merge to `main`, [semantic-release](https://github.com/semantic-release/semantic-release) analyses conventional commits, determines the next version, creates the version tag, and publishes a GitHub release.
4. [GoReleaser](https://goreleaser.com) builds multi-platform binaries and attaches them to the release.
5. After the release, a separate `update-provider-version-examples` workflow runs [`sync-terraform-version-in-docs`](https://github.com/alchemaxinc/composite-toolbox/tree/main/sync-terraform-version-in-docs) (from `alchemaxinc/composite-toolbox`) to rewrite the `~> N` provider version constraint in `README.md` and `examples/provider/provider.tf` to match the new major version, regenerates `docs/` with `tfplugindocs`, and opens an auto-merging pull request with the result. This keeps the examples on the newly published major version in sync, though the very next release after a major bump may briefly show a stale constraint until that pull request merges.

CI runs `sync-terraform-version-in-docs` in `check` mode on every pull request, verifying `README.md` and `examples/provider/provider.tf` agree on the same version constraint.

### Commit Convention

- `feat:` — new feature (minor bump)
- `fix:` — bug fix (patch bump)
- `feat!:` / `BREAKING CHANGE:` — breaking change (major bump)
- `chore:`, `docs:`, `ci:`, `refactor:` — no release
