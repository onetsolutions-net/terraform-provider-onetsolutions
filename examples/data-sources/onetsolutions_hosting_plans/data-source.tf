data "onetsolutions_hosting_plans" "all" {}

output "hosting_plan_slugs" {
  value = data.onetsolutions_hosting_plans.all.plans[*].slug
}
