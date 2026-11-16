resource "balena_release_tag" "example" {
  release_id = 123456
  tag_key    = "example-tag-key"
  value      = "example-value"
}
