data "onetsolutions_hosting_plans" "all" {}

locals {
  business = one([for p in data.onetsolutions_hosting_plans.all.plans : p if p.slug == "hebergement-business"])
}

# Creating this resource places an order billed on billing_cycle. Destroying it requests its
# cancellation at the end of the billing period: it keeps running, and billed, until then.
resource "onetsolutions_web_hosting" "site" {
  domain        = "example.fr"
  plan_id       = local.business.id
  billing_cycle = "annually"

  # The domain is registered elsewhere and pointed at the web hosting's name servers.
  domain_source = "external"

  deletion_protection = true

  # Sent with the cancellation once deletion_protection is lifted and the resource destroyed.
  cancellation_reason_code = "no_longer_needed"
  cancellation_reason      = "This site moves to a larger plan of another account."
}

output "nameservers" {
  value = onetsolutions_web_hosting.site.nameservers
}
