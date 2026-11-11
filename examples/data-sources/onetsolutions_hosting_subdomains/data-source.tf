data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_subdomains" "site" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

output "subdomains" {
  value = data.onetsolutions_hosting_subdomains.site.subdomains[*].full_domain
}
