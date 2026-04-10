data "balena_application" "my_app" {
  app_name = "my-iot-fleet"
}

output "app_slug" {
  value = data.balena_application.my_app.slug
}
