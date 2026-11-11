data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

variable "database_password" {
  type      = string
  sensitive = true
}

resource "onetsolutions_hosting_database_user" "app" {
  hosting_id = data.onetsolutions_web_hosting.site.id
  username   = "app"
  password   = var.database_password
}
