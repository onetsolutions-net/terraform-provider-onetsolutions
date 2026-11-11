# Every extension open to registration, with its one-year price.
data "onetsolutions_tlds" "open" {
  registration_available = true
}

output "one_year_prices" {
  value = {
    for t in data.onetsolutions_tlds.open.tlds :
    t.tld => one([for p in t.pricing : p.registration_price if p.years == 1])
  }
}
