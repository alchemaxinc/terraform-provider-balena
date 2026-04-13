resource "balena_release_tag" "version" {
  release_id = 123456
  tag_key    = "version"
  value      = "1.0.0"
}
