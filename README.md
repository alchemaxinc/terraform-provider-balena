# Terraform Provider for Balena Cloud

[![Semantic Versioning and Release](https://github.com/alchemaxinc/terraform-provider-balena/actions/workflows/semantic-release.yml/badge.svg)](https://github.com/alchemaxinc/terraform-provider-balena/actions/workflows/semantic-release.yml)

A Terraform provider for managing [Balena Cloud](https://balena.io) resources — fleets, environment variables, SSH keys, tags, and more.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.23 (to build from source)

## Authentication

The provider requires a Balena Cloud API token. You can create one in the [Balena Cloud dashboard](https://dashboard.balena-cloud.com/preferences/access-tokens).

Set the token via the environment variable:

```shell
export BALENA_API_TOKEN="your_balena_api_token"
```

Or configure it directly in the provider block (not recommended for secrets):

```hcl
provider "balena" {
  api_token = "your_balena_api_token"
}
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

## Resources

| Resource                        | Description                                                     |
| ------------------------------- | --------------------------------------------------------------- |
| `balena_application`            | Balena application (fleet)                                      |
| `balena_application_env_var`    | Application-level environment variable                          |
| `balena_application_config_var` | Application-level config variable (`RESIN_` / `BALENA_` prefix) |
| `balena_device_env_var`         | Device-level environment variable                               |
| `balena_device_service_env_var` | Device service-level environment variable                       |
| `balena_ssh_key`                | SSH public key associated with the account                      |
| `balena_application_tag`        | Tag on an application                                           |
| `balena_device_tag`             | Tag on a device                                                 |

## Data Sources

| Data Source          | Description                       |
| -------------------- | --------------------------------- |
| `balena_application` | Look up an application by name/ID |
| `balena_device`      | Look up a device by UUID          |

## Example Usage

```hcl
# Create a fleet
resource "balena_application" "my_fleet" {
  app_name        = "my-iot-fleet"
  device_type     = "raspberrypi4-64"
  organization_id = 123456
}

# Set an environment variable on the fleet
resource "balena_application_env_var" "mqtt_host" {
  application_id = balena_application.my_fleet.id
  name           = "MQTT_HOST"
  value          = "mqtt.example.com"
}

# Set a config variable
resource "balena_application_config_var" "persistent_logging" {
  application_id = balena_application.my_fleet.id
  name           = "BALENA_SUPERVISOR_PERSISTENT_LOGGING"
  value          = "true"
}

# Add SSH key
resource "balena_ssh_key" "ci_key" {
  title      = "CI Deploy Key"
  public_key = file("~/.ssh/id_ed25519.pub")
}

# Tag the fleet
resource "balena_application_tag" "env" {
  application_id = balena_application.my_fleet.id
  tag_key        = "environment"
  value          = "production"
}
```

## Development

### Building

```shell
make build
```

### Installing locally

```shell
make install
```

### Running tests

```shell
# Unit tests
make test

# Acceptance tests (requires BALENA_API_TOKEN)
make testacc
```

### Linting

```shell
make lint
```

### Generating docs

```shell
make docs
```

## Release Process

This repository follows semantic versioning with an automated release pipeline:

1. Development work happens on the `develop` branch.
2. A PR is raised from `develop` to `main`.
3. On merge to `main`, [semantic-release](https://github.com/semantic-release/semantic-release) analyses conventional commits and creates a version tag and GitHub release.
4. [GoReleaser](https://goreleaser.com) builds multi-platform binaries and attaches them to the release.
5. The `main` branch is backmerged into `develop` automatically.

### Commit Convention

This repository uses [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — new feature (minor bump)
- `fix:` — bug fix (patch bump)
- `feat!:` / `BREAKING CHANGE:` — breaking change (major bump)
- `chore:`, `docs:`, `ci:`, `refactor:` — no release

## License

[MIT](LICENSE)
