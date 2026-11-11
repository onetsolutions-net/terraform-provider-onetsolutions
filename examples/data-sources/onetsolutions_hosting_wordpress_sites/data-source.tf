data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_wordpress_sites" "site" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

# The import IDs of the existing sites, to import them as onetsolutions_hosting_wordpress.
output "wordpress_import_ids" {
  value = [
    for site in data.onetsolutions_hosting_wordpress_sites.site.wordpress_sites :
    "${data.onetsolutions_web_hosting.site.id}/${site.id}"
  ]
}
