data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

variable "ftp_password" {
  type      = string
  sensitive = true
}

resource "onetsolutions_hosting_ftp_account" "deploy" {
  hosting_id = data.onetsolutions_web_hosting.site.id
  username   = "deploy"
  password   = var.ftp_password
  directory  = "/public_html/app"
}

output "ftp_login" {
  value = onetsolutions_hosting_ftp_account.deploy.login
}
