resource "balena_device_tag" "example" {
  device_id = 123456
  tag_key   = "example-tag-key"
  value     = "example-value"
}
