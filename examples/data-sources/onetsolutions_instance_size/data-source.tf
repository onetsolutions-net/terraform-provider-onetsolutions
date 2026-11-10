data "onetsolutions_instance_size" "small" {
  slug = "p-4"
}

output "small_monthly_price" {
  value = one([for p in data.onetsolutions_instance_size.small.pricing : p.price if p.billing_cycle == "monthly"])
}
