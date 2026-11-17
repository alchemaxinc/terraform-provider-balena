resource "balena_device_profile_override" "example" {
  device_id           = 123456
  host_application_id = 123456
  profile_name        = "example-profile-name"
}
