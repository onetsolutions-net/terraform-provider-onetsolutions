data "onetsolutions_domain_availability" "example" {
  name = "example.com"
}

output "registrable" {
  value = data.onetsolutions_domain_availability.example.available
}

output "first_year_price" {
  value = data.onetsolutions_domain_availability.example.price
}

output "alternatives" {
  value = [for s in data.onetsolutions_domain_availability.example.suggestions : "${s.name} (${s.price} EUR)"]
}
