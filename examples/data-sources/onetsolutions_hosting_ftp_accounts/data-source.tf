data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_ftp_accounts" "all" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

output "ftp_accounts" {
  value = [for item in data.onetsolutions_hosting_ftp_accounts.all.ftp_accounts : item.username]
}
