resource "balena_device_service_environment_variable" "example" {
  name               = "MQTT_HOST"
  service_install_id = 123456
  value              = "example-value"
}
