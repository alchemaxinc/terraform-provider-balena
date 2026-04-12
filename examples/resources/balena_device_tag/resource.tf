resource "balena_device_tag" "role" {
  device_id = 123456
  tag_key   = "role"
  value     = "gateway"
}
