resource "balena_device_config_variable" "example" {
  device_id = 123456
  name      = "BALENA_HOST_CONFIG_gpu_mem"
  value     = "example-value"
}
