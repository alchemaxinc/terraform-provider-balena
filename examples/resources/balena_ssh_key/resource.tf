resource "balena_ssh_key" "ci_key" {
  title      = "CI Deploy Key"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... ci@example.com"
}
