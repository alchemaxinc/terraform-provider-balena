resource "balena_image_profile" "example" {
  release_image_id = data.balena_release_image.example.id
  profile_name     = "my-profile"
}
