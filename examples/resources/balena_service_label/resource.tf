resource "balena_service_label" "example" {
  label_name = "example-label-name"
  service_id = 123456
  value      = "example-value"
}
