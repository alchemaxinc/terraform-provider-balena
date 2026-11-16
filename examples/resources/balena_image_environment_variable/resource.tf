resource "balena_image_environment_variable" "example" {
  name             = "example-name"
  release_image_id = 123456
  value            = "example-value"
}
