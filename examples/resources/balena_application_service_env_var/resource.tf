resource "balena_application_service_env_var" "api_port" {
  service_id = 123456
  name       = "API_PORT"
  value      = "8080"
}
