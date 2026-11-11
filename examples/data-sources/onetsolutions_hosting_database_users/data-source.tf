data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_database_users" "all" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

output "database_users" {
  value = [for item in data.onetsolutions_hosting_database_users.all.database_users : item.username]
}
