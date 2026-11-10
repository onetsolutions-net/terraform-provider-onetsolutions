resource "onetsolutions_dns_zone" "example" {
  zone_name = "example.com"
}

resource "onetsolutions_dns_record" "apex" {
  zone_id = onetsolutions_dns_zone.example.id
  type    = "A"
  name    = "@"
  content = "203.0.113.10"
  ttl     = 300
}

resource "onetsolutions_dns_record" "www" {
  zone_id = onetsolutions_dns_zone.example.id
  type    = "CNAME"
  name    = "www"
  content = "example.com"
  proxied = true
}

resource "onetsolutions_dns_record" "mail" {
  zone_id  = onetsolutions_dns_zone.example.id
  type     = "MX"
  name     = "@"
  content  = "mail.example.net"
  priority = 10
}

resource "onetsolutions_dns_record" "spf" {
  zone_id = onetsolutions_dns_zone.example.id
  type    = "TXT"
  name    = "@"
  content = "v=spf1 mx -all"
}
