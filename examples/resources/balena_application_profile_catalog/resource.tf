resource "balena_application_profile_catalog" "gpu" {
  application_id = 123456
  profile_name   = "gpu"
  description    = "Enables GPU acceleration for the fleet."
}
