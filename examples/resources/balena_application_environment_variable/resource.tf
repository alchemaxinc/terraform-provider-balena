resource "balena_application_environment_variable" "example" {
  application_id = 123456
  name           = "MQTT_HOST"
  value          = "example-value"
}
