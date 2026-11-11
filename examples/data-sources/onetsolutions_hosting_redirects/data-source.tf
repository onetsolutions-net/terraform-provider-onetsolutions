data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_redirects" "site" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

output "redirects" {
  value = {
    for r in data.onetsolutions_hosting_redirects.site.redirects :
    "${r.domain}${r.source}" => r.destination
  }
}
