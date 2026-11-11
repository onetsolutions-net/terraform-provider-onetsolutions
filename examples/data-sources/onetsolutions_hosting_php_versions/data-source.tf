data "onetsolutions_hosting_php_versions" "site" {
  hosting_id = onetsolutions_web_hosting.site.id
}

output "php_versions" {
  value = [for v in data.onetsolutions_hosting_php_versions.site.versions : v.version]
}
