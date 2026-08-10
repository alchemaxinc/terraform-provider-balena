resource "balena_application_profile" "gpu" {
  application_id    = 1234567
  profile_name      = "gpu"
  on_application_id = 7654321
}
