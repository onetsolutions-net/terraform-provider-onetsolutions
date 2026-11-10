output "instance_ipv4" {
  description = "The public IPv4 address of each instance."
  value       = { for name, instance in onetsolutions_instance.web : name => instance.primary_ipv4 }
}

output "nameservers" {
  description = "Delegate the domain to these name servers at its registrar."
  value       = onetsolutions_dns_zone.main.nameservers
}
