# Contributing to Terraform Provider for Balena Cloud

Thank you for your interest in contributing! This document covers the process for contributing to this project.

## Requirements

- [Go](https://golang.org/doc/install)
- [Terraform](https://developer.hashicorp.com/terraform/downloads)
- [golangci-lint](https://golangci-lint.run/welcome/install-local/)
- [prettier](https://prettier.io/docs/en/install.html) (for formatting documentation)

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone git@github.com:YOUR_USER/terraform-provider-balena.git`
3. Create a branch: `git checkout -b feat/my-feature`
4. Make your changes
5. Run checks: `make lint && make test`
6. Commit using [Conventional Commits](https://www.conventionalcommits.org/): `git commit -m "feat: add widget resource"`
7. Push and open a pull request against `develop`
