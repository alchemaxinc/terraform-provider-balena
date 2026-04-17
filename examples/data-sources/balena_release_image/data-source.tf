data "balena_release_image" "api_build" {
  release_id = 1234567
  image_id   = 9876543
}

output "release_image_id" {
  value = data.balena_release_image.api_build.id
}
