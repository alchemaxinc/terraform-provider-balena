resource "balena_application_config_variable" "example" {
  application_id = 123456
  name           = "BALENA_HOST_CONFIG_gpu_mem"
  value          = "example-value"
}
