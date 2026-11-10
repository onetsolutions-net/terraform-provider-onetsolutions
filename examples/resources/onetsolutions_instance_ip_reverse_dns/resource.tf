# The hostname must already resolve to the address before its reverse DNS is set.
resource "onetsolutions_dns_record" "mail" {
  zone_id = onetsolutions_dns_zone.example.id
  type    = "A"
  name    = "mail"
  content = onetsolutions_instance.mail.primary_ipv4
}

resource "onetsolutions_instance_ip_reverse_dns" "mail" {
  instance_id = onetsolutions_instance.mail.id
  address     = onetsolutions_instance.mail.primary_ipv4
  reverse_dns = "mail.example.com"

  depends_on = [onetsolutions_dns_record.mail]
}
