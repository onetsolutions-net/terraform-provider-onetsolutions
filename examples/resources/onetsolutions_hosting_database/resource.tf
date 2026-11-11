data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

resource "onetsolutions_hosting_database" "shop" {
  hosting_id          = data.onetsolutions_web_hosting.site.id
  name                = "shop"
  deletion_protection = true
}

output "database_name" {
  value = onetsolutions_hosting_database.shop.full_name
}
