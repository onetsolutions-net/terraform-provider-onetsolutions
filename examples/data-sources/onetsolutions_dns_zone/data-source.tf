data "onetsolutions_dns_zone" "example" {
  zone_name = "example.com"
}

output "example_nameservers" {
  value = data.onetsolutions_dns_zone.example.nameservers
}
