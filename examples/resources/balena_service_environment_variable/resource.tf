resource "balena_service_environment_variable" "example" {
  name       = "MQTT_HOST"
  service_id = 123456
  value      = "example-value"
}
