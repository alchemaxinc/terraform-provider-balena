resource "balena_application_tag" "example" {
  application_id = 123456
  tag_key        = "example-tag-key"
  value          = "example-value"
}
