data "onetsolutions_domain" "example" {
  name = "example.fr"
}

output "example_expires_on" {
  value = data.onetsolutions_domain.example.expiration_date
}
