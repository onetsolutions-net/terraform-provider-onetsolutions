data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_databases" "all" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

output "databases" {
  value = [for item in data.onetsolutions_hosting_databases.all.databases : item.name]
}
