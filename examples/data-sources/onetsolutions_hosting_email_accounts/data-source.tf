data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_email_accounts" "all" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

output "email_accounts" {
  value = [for item in data.onetsolutions_hosting_email_accounts.all.email_accounts : item.email]
}
