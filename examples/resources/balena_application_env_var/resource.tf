resource "balena_application_env_var" "mqtt_host" {
  application_id = 123456
  name           = "MQTT_HOST"
  value          = "mqtt.example.com"
}
