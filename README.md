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

- `internal/balena/` — thin HTTP client for the Balena Cloud REST API (`/v6/...`).
- `internal/provider/` — Terraform Plugin Framework provider, resources, and data sources.
- `schema/` — the pinned upstream release (`VERSION`), the extraction script and the normalized `/v6` API model (`api/`) that code is generated from. See [`schema/README.md`](schema/README.md).
- `internal/schemagen/`, `internal/engine/`, `internal/generated/` — the generator, the generic resource engine and the generated descriptors.
- `docs/` — auto-generated provider documentation (do not edit manually; regenerate with `make docs`).
- `examples/` — Terraform example snippets for every resource and data source.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, build commands, and release process.

## Looking for a Balena MCP provider?

Check out the [Balena MCP client](https://github.com/kieksme/mcp-balena) from 👉[kieks.me](https://kieks.me/).

## License

[MIT](LICENSE)
