data "balena_device" "my_device" {
  uuid = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
}

output "device_name" {
  value = data.balena_device.my_device.device_name
}

output "device_ip" {
  value = data.balena_device.my_device.ip_address
}
