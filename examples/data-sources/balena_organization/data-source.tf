data "balena_organization" "my_org" {
  handle = "my-org-handle"
}

output "organization_id" {
  value = data.balena_organization.my_org.id
}
