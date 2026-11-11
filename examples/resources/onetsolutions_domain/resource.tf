# Registers example.com for one year: a billed order that cannot be undone.
resource "onetsolutions_domain" "example" {
  name = "example.com"

  lifecycle {
    prevent_destroy = true
  }
}

resource "onetsolutions_dns_zone" "example" {
  zone_name = onetsolutions_domain.example.name
}

# The settings of the registered domain: delegation to the zone, renewal, lock and privacy.
resource "onetsolutions_domain_settings" "example" {
  domain_id     = onetsolutions_domain.example.id
  nameservers   = onetsolutions_dns_zone.example.nameservers
  auto_renew    = true
  locked        = true
  whois_privacy = true
}
