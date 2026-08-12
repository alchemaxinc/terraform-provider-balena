resource "balena_application_profile" "gpu" {
  application_id      = 123456
  profile_name        = "gpu"
  host_application_id = 654321
}
