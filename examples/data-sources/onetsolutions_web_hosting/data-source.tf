data "onetsolutions_web_hosting" "shop" {
  domain = "shop.example.fr"
}

data "onetsolutions_dns_zone" "example" {
  zone_name = "example.fr"
}

# Point a record of a zone at the web hosting's dedicated address.
resource "onetsolutions_dns_record" "shop" {
  zone_id = data.onetsolutions_dns_zone.example.id
  type    = "A"
  name    = "shop"
  content = data.onetsolutions_web_hosting.shop.dedicated_ip
}
