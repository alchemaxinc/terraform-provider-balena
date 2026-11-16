resource "balena_application" "example" {
  app_name        = "example-app-name"
  device_type     = "raspberrypi4-64"
  organization_id = 123456
}
