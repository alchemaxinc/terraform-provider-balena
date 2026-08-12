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
      version = "~> 1"
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
- `schema/` — reference copy of the upstream Balena Pine.js SBVR schema (`balena.sbvr`), used when authoring new resources to confirm field names and relationships. Not consumed at runtime.
- `docs/` — auto-generated provider documentation (do not edit manually; regenerate with `make docs`).
- `examples/` — Terraform example snippets for every resource and data source.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, build commands, and release process.

## License

[MIT](LICENSE)
