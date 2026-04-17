data "balena_application" "my_app" {
  app_name = "my-iot-fleet"
}

data "balena_service" "api" {
  application_id = data.balena_application.my_app.id
  service_name   = "api"
}

output "service_id" {
  value = data.balena_service.api.id
}
