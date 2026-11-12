resource "balena_device" "gateway" {
  uuid           = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
  device_name    = "gateway-1"
  pinned_release = "f2a97d4338b09ef613a17961f4fafbc2"
  os_version     = "6.0.10+rev1"
}
