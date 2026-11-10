data "onetsolutions_domain" "example" {
  name = "example.com"
}

resource "onetsolutions_dns_zone" "example" {
  zone_name = "example.com"
}

# Delegate the domain to its zone, keep it locked, hide the registrant from WHOIS.
resource "onetsolutions_domain_settings" "example" {
  domain_id     = data.onetsolutions_domain.example.id
  nameservers   = onetsolutions_dns_zone.example.nameservers
  auto_renew    = true
  locked        = true
  whois_privacy = true
}
