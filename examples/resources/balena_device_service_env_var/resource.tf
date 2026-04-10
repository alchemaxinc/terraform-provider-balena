resource "balena_device_service_env_var" "log_level" {
  service_install_id = 789012
  name               = "LOG_LEVEL"
  value              = "debug"
}
