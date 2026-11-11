data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_addon_domains" "site" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

output "addon_domains" {
  value = data.onetsolutions_hosting_addon_domains.site.addon_domains[*].domain
}
