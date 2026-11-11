data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

resource "onetsolutions_hosting_addon_domain" "shop" {
  hosting_id    = data.onetsolutions_web_hosting.site.id
  domain        = "example-shop.fr"
  document_root = "public_html/example-shop.fr"
}
