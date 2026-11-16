# Terraform Provider for Balena Cloud

[![Semantic Versioning and Release](https://github.com/alchemaxinc/terraform-provider-balena/actions/workflows/semantic-release.yml/badge.svg)](https://github.com/alchemaxinc/terraform-provider-balena/actions/workflows/semantic-release.yml)

A Terraform provider for managing [Balena Cloud](https://balena.io) resources — fleets, environment variables, SSH keys, tags, and more.

This provider is a reflection of the [Balena API](https://docs.balena.io/reference/api/resources).

## Authentication

The provider requires a Balena Cloud API token. You can create one in the [Balena Cloud dashboard](https://dashboard.balena-cloud.com/preferences/access-tokens).

Set the token via the environment variable:

```shell
export BALENA_API_TOKEN="your_balena_api_token"
```

## Provider Configuration

```hcl
terraform {
  required_providers {
    balena = {
      source  = "alchemaxinc/balena"
      version = "~> 2"
    }
  }
}

provider "balena" {
  # api_token is read from BALENA_API_TOKEN env var if not set here
  # api_url defaults to https://api.balena-cloud.com
}
```

## Resources & Data Sources

See the [`docs/`](docs/) directory for full schema documentation and the [`examples/`](examples/) directory for usage examples.

## Repository Layout

- `schema/` — pinned upstream balena models, the one hand-edited `overrides.yaml`, and the generated IR, surface lock
  and coverage report. See [schema/README.md](schema/README.md).
- `cmd/balena-schema/`, `internal/gen/` — the generator: `make fetch`, `make generate`, `make check-generate`,
  `make surface-diff`, `make contract`.
- `internal/balena/` — a generic Pine.js OData client for the balena API (`/v7`, and `/resin` for preview resources).
- `internal/provider/` — one generic resource and one generic data source driven by the generated specs
  (`zz_generated_specs.go`), plus the named hooks in `hooks.go`.
- `docs/` — generated provider documentation (regenerate with `make docs`).
- `examples/` — generated example for every resource and data source.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, build commands, and release process.

## Looking for a Balena MCP provider?

Check out the [Balena MCP client](https://github.com/kieksme/mcp-balena) from 👉[kieks.me](https://kieks.me/).

## License

[MIT](LICENSE)
