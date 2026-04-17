data "balena_device" "pi" {
  uuid = "0123456789abcdef0123456789abcdef"
}

data "balena_service" "api" {
  application_id = data.balena_device.pi.application_id
  service_name   = "api"
}

data "balena_service_install" "pi_api" {
  device_id  = data.balena_device.pi.id
  service_id = data.balena_service.api.id
}

resource "balena_device_service_env_var" "log_level" {
  service_install_id = data.balena_service_install.pi_api.id
  name               = "LOG_LEVEL"
  value              = "debug"
}
