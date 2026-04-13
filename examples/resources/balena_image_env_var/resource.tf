resource "balena_image_env_var" "node_env" {
  release_image_id = 789012
  name             = "NODE_ENV"
  value            = "production"
}
