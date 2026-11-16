resource "balena_device_environment_variable" "example" {
  device_id = 123456
  name      = "MQTT_HOST"
  value     = "example-value"
}
