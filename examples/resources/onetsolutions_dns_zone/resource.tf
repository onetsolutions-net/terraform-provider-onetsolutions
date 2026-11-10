resource "onetsolutions_dns_zone" "example" {
  zone_name = "example.com"
}

# Delegate the domain to these name servers at its registrar.
output "example_nameservers" {
  value = onetsolutions_dns_zone.example.nameservers
}
