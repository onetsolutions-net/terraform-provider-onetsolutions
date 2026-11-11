data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

resource "onetsolutions_hosting_redirect" "old_page" {
  hosting_id  = data.onetsolutions_web_hosting.site.id
  domain      = "example.com"
  source      = "/old-page"
  destination = "https://example.com/new-page"
}

# Redirect a whole domain, temporarily.
resource "onetsolutions_hosting_redirect" "campaign" {
  hosting_id  = data.onetsolutions_web_hosting.site.id
  domain      = "promo.example.com"
  source      = "/"
  destination = "https://example.com/offers"
  type        = "302"
}
