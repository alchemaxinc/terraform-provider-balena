resource "balena_device_profile_override" "gpu" {
  device_id           = 987654
  profile_name        = "gpu"
  host_application_id = 654321
  is_active           = true
}
