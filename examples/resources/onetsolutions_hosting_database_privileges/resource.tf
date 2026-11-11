data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

variable "database_password" {
  type      = string
  sensitive = true
}

resource "onetsolutions_hosting_database" "shop" {
  hosting_id = data.onetsolutions_web_hosting.site.id
  name       = "shop"
}

resource "onetsolutions_hosting_database_user" "app" {
  hosting_id = data.onetsolutions_web_hosting.site.id
  username   = "app"
  password   = var.database_password
}

resource "onetsolutions_hosting_database_privileges" "app_on_shop" {
  hosting_id = data.onetsolutions_web_hosting.site.id
  database   = onetsolutions_hosting_database.shop.name
  username   = onetsolutions_hosting_database_user.app.username
  privileges = ["SELECT", "INSERT", "UPDATE", "DELETE"]

  lifecycle {
    # A new password recreates the user, which drops its privileges: grant them again.
    replace_triggered_by = [onetsolutions_hosting_database_user.app]
  }
}
