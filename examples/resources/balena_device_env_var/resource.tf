resource "balena_device_env_var" "debug_mode" {
  device_id = 123456
  name      = "DEBUG"
  value     = "true"
}
