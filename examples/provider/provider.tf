terraform {
  required_providers {
    balena = {
      source  = "alchemaxinc/balena"
      version = "~> 1"
    }
  }
}

provider "balena" {
  # api_token is read from BALENA_API_TOKEN environment variable.
  # api_url defaults to https://api.balena-cloud.com
}
