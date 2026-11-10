data "onetsolutions_organizations" "all" {}

output "organization_names" {
  value = data.onetsolutions_organizations.all.organizations[*].name
}
