data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

# Every night at 03:15.
resource "onetsolutions_hosting_cron_job" "backup" {
  hosting_id = data.onetsolutions_web_hosting.site.id
  command    = "php -q /home/account/public_html/backup.php"
  minute     = "15"
  hour       = "3"
}

# Every five minutes on weekdays.
resource "onetsolutions_hosting_cron_job" "queue" {
  hosting_id = data.onetsolutions_web_hosting.site.id
  command    = "php -q /home/account/public_html/queue.php"
  minute     = "*/5"
  weekday    = "1-5"
}
