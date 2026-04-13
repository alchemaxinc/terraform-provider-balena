resource "balena_service_label" "restart_policy" {
  service_id = 123456
  label_name = "io.balena.features.supervisor-api"
  value      = "true"
}
