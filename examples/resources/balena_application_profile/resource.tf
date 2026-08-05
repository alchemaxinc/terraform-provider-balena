resource "balena_application_profile" "example" {
  application_id = balena_application.fleet.id
  host_app_id    = data.balena_application.host.id
  profile_name   = "my-profile"
}
