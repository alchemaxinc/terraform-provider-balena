resource "balena_device_config_var" "gpu_memory" {
  device_id = 123456
  name      = "BALENA_HOST_CONFIG_gpu_mem"
  value     = "128"
}
