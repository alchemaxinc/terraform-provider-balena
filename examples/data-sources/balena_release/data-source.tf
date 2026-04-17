data "balena_application" "my_app" {
  app_name = "my-iot-fleet"
}

data "balena_release" "current" {
  application_id = data.balena_application.my_app.id
  commit         = "abc1234def5678"
}

output "release_status" {
  value = data.balena_release.current.status
}
