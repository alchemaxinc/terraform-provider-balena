resource "balena_application_config_var" "persistent_logging" {
  application_id = 123456
  name           = "BALENA_SUPERVISOR_PERSISTENT_LOGGING"
  value          = "true"
}
