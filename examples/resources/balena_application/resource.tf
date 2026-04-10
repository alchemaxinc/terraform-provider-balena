resource "balena_application" "my_fleet" {
  app_name        = "my-iot-fleet"
  device_type     = "raspberrypi4-64"
  organization_id = 123456
  is_public       = false
}
