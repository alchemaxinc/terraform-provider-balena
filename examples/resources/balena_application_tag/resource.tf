resource "balena_application_tag" "environment" {
  application_id = 123456
  tag_key        = "environment"
  value          = "production"
}
