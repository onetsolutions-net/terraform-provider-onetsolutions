data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

variable "mailbox_password" {
  type      = string
  sensitive = true
}

resource "onetsolutions_hosting_email_account" "contact" {
  hosting_id = data.onetsolutions_web_hosting.site.id
  email      = "contact@example.com"
  password   = var.mailbox_password
  quota_mb   = 1024
}
