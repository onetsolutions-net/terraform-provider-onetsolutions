data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

resource "onetsolutions_hosting_subdomain" "blog" {
  hosting_id    = data.onetsolutions_web_hosting.site.id
  subdomain     = "blog"
  document_root = "public_html/blog"
}
