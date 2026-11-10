resource "onetsolutions_dns_zone" "example" {
  zone_name = "example.com"
}

resource "onetsolutions_dns_zone_dnssec" "example" {
  zone_id = onetsolutions_dns_zone.example.id
  enabled = true
}

# Publish this DS record at the registrar, unless automatic_ds_publication is true.
output "example_ds_record" {
  value = onetsolutions_dns_zone_dnssec.example.ds_record
}
